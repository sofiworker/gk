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

type Handler struct {
	desc    *ServiceDesc
	impl    any
	options serviceOptions
}

func (f *Fault) Error() string {
	if f == nil {
		return "soap fault: <nil>"
	}
	if f.Code == "" {
		return f.String
	}
	return fmt.Sprintf("soap fault (code=%s): %s", f.Code, f.String)
}

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

	payload, wrapper, err := decodeSOAPBodyPayload(data)
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
	if req != nil && len(bytes.TrimSpace(payload)) > 0 {
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

	if err := h.writeOperationResponse(w, op.Operation.SOAPVersion, resp); err != nil {
		h.writeServerFault(w, fmt.Errorf("encode operation response: %w", err), op.Operation.SOAPVersion)
		return
	}
}

func (h *Handler) writeOperationResponse(w http.ResponseWriter, version SOAPVersion, resp any) error {
	soapEnv, err := h.resolveSOAPEnvelope(version)
	if err != nil {
		return err
	}

	data, err := marshalEnvelope(envelope{
		SoapEnv: soapEnv,
		Body: body{
			Content: resp,
		},
	})
	if err != nil {
		return err
	}

	writeXML(w, http.StatusOK, data)
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

	soapEnv, err := h.resolveSOAPEnvelope(version)
	if err != nil {
		soapEnv = SOAP11EnvelopeNamespace
	}

	envFault := &envelopeFault{
		Code:   fault.Code,
		String: fault.String,
		Actor:  fault.Actor,
	}
	if detail := marshalFaultDetail(fault.Detail); detail != "" {
		envFault.Detail = &faultDetail{InnerXML: detail}
	}

	data, err := marshalEnvelope(envelope{
		SoapEnv: soapEnv,
		Body: body{
			Fault: envFault,
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeXML(w, http.StatusInternalServerError, data)
}

func marshalFaultDetail(v any) string {
	switch detail := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(detail)
	case []byte:
		return strings.TrimSpace(string(detail))
	default:
		data, err := xml.Marshal(detail)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}

func writeXML(w http.ResponseWriter, statusCode int, data []byte) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}
