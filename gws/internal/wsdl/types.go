package wsdl

import "errors"

var ErrUnsupportedBindingStyle = errors.New("unsupported binding style")
var ErrUnsupportedXSDChoice = errors.New("unsupported xsd:choice")
