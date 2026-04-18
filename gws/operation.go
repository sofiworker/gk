package gws

import "encoding/xml"

type Operation struct {
	Name            string
	Action          string
	RequestWrapper  xml.Name
	ResponseWrapper xml.Name
	SOAPVersion     SOAPVersion
}
