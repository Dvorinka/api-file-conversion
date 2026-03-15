package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"apiservices/file-conversion/internal/convert/converter"
)

type Handler struct {
	service *converter.Service
}

func NewHandler(service *converter.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/v1/convert/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/convert/"), "/")
	switch path {
	case "file":
		h.handleConvertFile(w, r)
	case "batch":
		h.handleConvertBatch(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleConvertFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := r.ParseMultipartForm(h.service.MaxFileSize() + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form data")
		return
	}

	targetFormat := strings.TrimSpace(r.FormValue("target_format"))
	if targetFormat == "" {
		writeError(w, http.StatusBadRequest, "target_format is required")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, h.service.MaxFileSize()+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	if int64(len(data)) > h.service.MaxFileSize() {
		writeError(w, http.StatusBadRequest, "file too large")
		return
	}

	result, err := h.service.ConvertBytes(r.Context(), header.Filename, targetFormat, data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) handleConvertBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Jobs []converter.JobInput `json:"jobs"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Jobs) == 0 {
		writeError(w, http.StatusBadRequest, "jobs cannot be empty")
		return
	}
	if len(req.Jobs) > 20 {
		writeError(w, http.StatusBadRequest, "max 20 jobs per request")
		return
	}

	results := make([]converter.ConversionResult, 0, len(req.Jobs))
	for _, job := range req.Jobs {
		results = append(results, h.service.ConvertBase64Job(r.Context(), job))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": results})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to marshal response"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return errors.New("invalid json body")
	}

	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("json body must contain a single object")
	}
	return nil
}
