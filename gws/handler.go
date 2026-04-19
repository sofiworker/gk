package gws

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Handler exposes a SOAP service through the standard net/http.Handler
// interface.
type Handler struct {
	desc    *ServiceDesc
	impl    any
	options serviceOptions
}

// Error implements the error interface for Fault values.
func (f *Fault) Error() string {
	if f == nil {
		return "soap fault: <nil>"
	}
	if f.Code == "" {
		return f.String
	}
	return fmt.Sprintf("soap fault (code=%s): %s", f.Code, f.String)
}

// NewHandler builds a standard net/http SOAP handler from a service
// description and implementation object.
func NewHandler(desc *ServiceDesc, impl any, opts ...ServiceOption) (*Handler, error) {
	if desc == nil {
		return nil, ErrNilServiceDesc
	}

	options := applyServiceOptions(opts...)
	if options.SOAPVersion == "" {
		options.SOAPVersion = SOAP11
	}

	return &Handler{
		desc:    desc,
		impl:    impl,
		options: options,
	}, nil
}

// ServeHTTP serves SOAP POST requests and optional WSDL/XSD GET endpoints.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.serveGET(w, r)
	case http.MethodPost:
		h.servePOST(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) serveGET(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if _, ok := query["wsdl"]; ok {
		data, exists := h.desc.wsdlAsset()
		if !exists {
			http.NotFound(w, r)
			return
		}
		writeXML(w, http.StatusOK, data)
		return
	}

	xsd := strings.TrimSpace(query.Get("xsd"))
	if xsd != "" {
		data, exists := h.desc.xsdAsset(xsd)
		if !exists {
			http.NotFound(w, r)
			return
		}
		writeXML(w, http.StatusOK, data)
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (h *Handler) servePOST(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeClientFault(w, fmt.Errorf("read request body: %w", err))
		return
	}

	payload, wrapper, err := DecodeBodyPayload(data)
	if err != nil {
		h.writeClientFault(w, fmt.Errorf("decode soap body: %w", err))
		return
	}

	op, found := h.desc.findOperationByWrapper(wrapper)
	if !found {
		h.writeFault(w, Fault{
			Code:   "soap:Client",
			String: ErrOperationNotFound.Error(),
		}, h.options.SOAPVersion)
		return
	}

	req := op.buildRequest()
	hasPayload := len(bytes.TrimSpace(payload)) > 0
	if hasPayload && req == nil {
		h.writeClientFault(w, ErrMissingRequestFactory)
		return
	}

	if req != nil && hasPayload {
		if err := xml.Unmarshal(payload, req); err != nil {
			h.writeClientFault(w, fmt.Errorf("decode operation request: %w", err))
			return
		}
	}

	resp, err := op.invoke(r.Context(), h.impl, req)
	if err != nil {
		h.writeInvokeFault(w, err, op.Operation.SOAPVersion)
		return
	}

	if err := h.writeOperationResponse(w, op.Operation, resp); err != nil {
		h.writeServerFault(w, fmt.Errorf("encode operation response: %w", err), op.Operation.SOAPVersion)
		return
	}
}

func (h *Handler) writeOperationResponse(w http.ResponseWriter, operation Operation, resp any) error {
	if err := validateOperationResponseWrapper(operation.ResponseWrapper, resp); err != nil {
		return err
	}

	soapEnv, err := h.resolveSOAPEnvelope(operation.SOAPVersion)
	if err != nil {
		return err
	}

	data, err := MarshalEnvelope(Envelope{
		Namespace: soapEnv,
		Body: Body{
			Content: resp,
		},
	})
	if err != nil {
		return err
	}

	writeXML(w, http.StatusOK, data)
	return nil
}

func validateOperationResponseWrapper(expectWrapper xml.Name, resp any) error {
	actualWrapper, err := requestBodyWrapperName(resp)
	if err != nil {
		return err
	}

	if err := checkResponseWrapper(expectWrapper, actualWrapper); err != nil {
		return err
	}

	return nil
}

func (h *Handler) resolveSOAPEnvelope(version SOAPVersion) (string, error) {
	if version == "" {
		version = h.options.SOAPVersion
	}
	return resolveSOAPEnvelopeNamespace(version)
}

func (h *Handler) writeClientFault(w http.ResponseWriter, err error) {
	h.writeServerOrClientFault(w, err, "soap:Client", h.options.SOAPVersion)
}

func (h *Handler) writeServerFault(w http.ResponseWriter, err error, version SOAPVersion) {
	h.writeServerOrClientFault(w, err, "soap:Server", version)
}

func (h *Handler) writeServerOrClientFault(w http.ResponseWriter, err error, code string, version SOAPVersion) {
	if err == nil {
		err = errors.New("unknown error")
	}

	h.writeFault(w, Fault{
		Code:   code,
		String: err.Error(),
	}, version)
}

func (h *Handler) writeInvokeFault(w http.ResponseWriter, err error, version SOAPVersion) {
	var fault *Fault
	if errors.As(err, &fault) && fault != nil {
		h.writeFault(w, *fault, version)
		return
	}

	h.writeServerFault(w, err, version)
}

func (h *Handler) writeFault(w http.ResponseWriter, fault Fault, version SOAPVersion) {
	if fault.Code == "" {
		fault.Code = "soap:Server"
	}
	if fault.String == "" {
		fault.String = "internal error"
	}

	if _, err := h.resolveSOAPEnvelope(version); err != nil {
		version = SOAP11
	}

	data, err := MarshalFaultEnvelope(fault, version)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeXML(w, http.StatusInternalServerError, data)
}

func writeXML(w http.ResponseWriter, statusCode int, data []byte) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}
