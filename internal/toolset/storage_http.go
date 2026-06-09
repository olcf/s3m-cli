//nolint:dupl // HTTP handlers follow same pattern
package toolset

import "net/http"

//
// HTTP handlers

func (h *storageHandlers) listDatasetsHTTP(w http.ResponseWriter, r *http.Request) {
	if !ensureMethodAndCORS(w, r, http.MethodPost) {
		return
	}

	var input listDatasetsRequest
	if err := decodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.handleListDatasets(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, resp)
}

func (h *storageHandlers) listFilesHTTP(w http.ResponseWriter, r *http.Request) {
	if !ensureMethodAndCORS(w, r, http.MethodPost) {
		return
	}

	var input listFilesRequest
	if err := decodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.handleListFiles(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, resp)
}

func (h *storageHandlers) readFileHTTP(w http.ResponseWriter, r *http.Request) {
	if !ensureMethodAndCORS(w, r, http.MethodPost) {
		return
	}

	var input readFileRequest
	if err := decodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.handleReadFile(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, resp)
}

func (h *storageHandlers) getDownloadURLHTTP(w http.ResponseWriter, r *http.Request) {
	if !ensureMethodAndCORS(w, r, http.MethodPost) {
		return
	}

	var input getDownloadURLRequest
	if err := decodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.handleGetDownloadURL(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, resp)
}

func (h *storageHandlers) putFileHTTP(w http.ResponseWriter, r *http.Request) {
	if !ensureMethodAndCORS(w, r, http.MethodPost) {
		return
	}

	var input putFileRequest
	if err := decodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.handlePutFile(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, resp)
}

func (h *storageHandlers) deleteDatasetHTTP(w http.ResponseWriter, r *http.Request) {
	if !ensureMethodAndCORS(w, r, http.MethodPost) {
		return
	}

	var input deleteDatasetRequest
	if err := decodeJSONBody(r.Body, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.handleDeleteDataset(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, resp)
}
