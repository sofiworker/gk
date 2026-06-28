package gws

import "encoding/xml"

// Operation describes a SOAP operation contract used by requests, clients and
// service descriptors.
type Operation struct {
	Name            string
	Action          string
	RequestWrapper  xml.Name
	ResponseWrapper xml.Name
	SOAPVersion     SOAPVersion
}
