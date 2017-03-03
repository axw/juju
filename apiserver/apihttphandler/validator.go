package apihttphandler

import "github.com/juju/juju/apiserver/params"

// LoginValidator functions are used to decide whether login requests
// are to be allowed. The validator is called before credentials are
// checked.
type LoginValidator func(params.LoginRequest) error
