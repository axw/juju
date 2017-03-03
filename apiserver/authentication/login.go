package authentication

import (
	"time"

	"github.com/juju/errors"
	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/permission"
	"github.com/juju/juju/state"
	"gopkg.in/juju/names.v2"
)

// Login validates the entity's credentials in the current model.
// If the entity is a user, and lookForModelUser is true, a model
// user must exist for the model. In the case of a user logging
// in to the controller, but not a model, there is no model user
// needed. While we have the model user, if we do have it, update
// the last login time.
//
// Note that when logging in with lookForModelUser true, the returned
// entity will be modelUserEntity, not *state.User (external users
// don't have user entries) or *state.ModelUser (we don't want to
// lose the local user information associated with that).
func Login(
	st *state.State,
	req params.LoginRequest,
	lookForModelUser bool,
	authenticator EntityAuthenticator,
) (state.Entity, *time.Time, error) {
	var tag names.Tag
	if req.AuthTag != "" {
		var err error
		tag, err = names.ParseTag(req.AuthTag)
		if err != nil {
			return nil, nil, errors.Trace(err)
		}
	}
	var entityFinder EntityFinder = st
	if lookForModelUser {
		// When looking up model users, use a custom
		// entity finder that looks up both the local user (if the user
		// tag is in the local domain) and the model user.
		entityFinder = modelUserEntityFinder{st}
	}
	entity, err := authenticator.Authenticate(entityFinder, tag, req)
	if err != nil {
		return nil, nil, errors.Trace(err)
	}

	// For user logins, update the last login time.
	var lastLogin *time.Time
	if entity, ok := entity.(loginEntity); ok {
		userLastLogin, err := entity.LastLogin()
		if err != nil && !state.IsNeverLoggedInError(err) {
			return nil, nil, errors.Trace(err)
		}
		entity.UpdateLastLogin()
		lastLogin = &userLastLogin
	}
	return entity, lastLogin, nil
}

// LoginControllerMachine checks the special case of a controller
// machine creating an API connection for a different model so it
// can run workers that act on behalf of a hosted model.
func LoginControllerMachine(
	controllerSt *state.State,
	req params.LoginRequest,
	authenticator EntityAuthenticator,
) (state.Entity, error) {
	entity, _, err := Login(controllerSt, req, false, authenticator)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if machine, ok := entity.(*state.Machine); !ok {
		return nil, errors.Errorf("entity should be a machine, but is %T", entity)
	} else if !machine.IsManager() {
		// The machine exists in the controller model, but it doesn't
		// manage models, so reject it.
		return nil, errors.Trace(common.ErrPerm)
	}
	return entity, nil
}

// loginEntity defines the interface needed to log in as a user.
// Notable implementations are *state.User and *modelUserEntity.
type loginEntity interface {
	state.Entity
	state.Authenticator
	LastLogin() (time.Time, error)
	UpdateLastLogin() error
}

// modelUserEntityFinder implements EntityFinder by returning a
// loginEntity value for users, ensuring that the user exists in the
// state's current model as well as retrieving more global
// authentication details such as the password.
type modelUserEntityFinder struct {
	st *state.State
}

// FindEntity implements EntityFinder.FindEntity.
func (f modelUserEntityFinder) FindEntity(tag names.Tag) (state.Entity, error) {
	utag, ok := tag.(names.UserTag)
	if !ok {
		return f.st.FindEntity(tag)
	}

	modelUser, controllerUser, err := common.UserAccess(f.st, utag)
	if err != nil {
		return nil, errors.Trace(err)
	}
	u := &modelUserEntity{
		st:             f.st,
		modelUser:      modelUser,
		controllerUser: controllerUser,
	}
	if utag.IsLocal() {
		user, err := f.st.User(utag)
		if err != nil {
			return nil, errors.Trace(err)
		}
		u.user = user
	}
	return u, nil
}

var _ loginEntity = &modelUserEntity{}

// modelUserEntity encapsulates an model user
// and, if the user is local, the local state user
// as well. This enables us to implement FindEntity
// in such a way that the authentication mechanisms
// can work without knowing these details.
type modelUserEntity struct {
	st *state.State

	controllerUser permission.UserAccess
	modelUser      permission.UserAccess
	user           *state.User
}

// Refresh implements state.Authenticator.Refresh.
func (u *modelUserEntity) Refresh() error {
	if u.user == nil {
		return nil
	}
	return u.user.Refresh()
}

// SetPassword implements state.Authenticator.SetPassword
// by setting the password on the local user.
func (u *modelUserEntity) SetPassword(pass string) error {
	if u.user == nil {
		return errors.New("cannot set password on external user")
	}
	return u.user.SetPassword(pass)
}

// PasswordValid implements state.Authenticator.PasswordValid.
func (u *modelUserEntity) PasswordValid(pass string) bool {
	if u.user == nil {
		return false
	}
	return u.user.PasswordValid(pass)
}

// Tag implements state.Entity.Tag.
func (u *modelUserEntity) Tag() names.Tag {
	if u.user != nil {
		return u.user.UserTag()
	}
	if !permission.IsEmptyUserAccess(u.modelUser) {
		return u.modelUser.UserTag
	}
	return u.controllerUser.UserTag
}

// LastLogin implements loginEntity.LastLogin.
func (u *modelUserEntity) LastLogin() (time.Time, error) {
	// The last connection for the model takes precedence over
	// the local user last login time.
	var err error
	var t time.Time
	if !permission.IsEmptyUserAccess(u.modelUser) {
		t, err = u.st.LastModelConnection(u.modelUser.UserTag)
	} else {
		err = state.NeverConnectedError("controller user")
	}
	if state.IsNeverConnectedError(err) || permission.IsEmptyUserAccess(u.modelUser) {
		if u.user != nil {
			// There's a global user, so use that login time instead.
			return u.user.LastLogin()
		}
		// Since we're implementing LastLogin, we need
		// to implement LastLogin error semantics too.
		err = state.NeverLoggedInError(err.Error())
	}
	return t, errors.Trace(err)
}

// UpdateLastLogin implements loginEntity.UpdateLastLogin.
func (u *modelUserEntity) UpdateLastLogin() error {
	var err error

	if !permission.IsEmptyUserAccess(u.modelUser) {
		if u.modelUser.Object.Kind() != names.ModelTagKind {
			return errors.NotValidf("%s as model user", u.modelUser.Object.Kind())
		}

		err = u.st.UpdateLastModelConnection(u.modelUser.UserTag)
	}

	if u.user != nil {
		err1 := u.user.UpdateLastLogin()
		if err == nil {
			return err1
		}
	}
	if err != nil {
		return errors.Trace(err)
	}
	return nil
}
