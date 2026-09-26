package api

import (
	"net/http"

	"github.com/wyw14/solo-035/internal/model"
)

func (server *Server) qualityRules(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"rules": server.runtime.Rulebook.ListAll()})
}

func (server *Server) qualityPermits(writer http.ResponseWriter, request *http.Request) {
	eventID := model.EventID(request.URL.Query().Get("event_id"))
	writeJSON(writer, http.StatusOK, map[string]any{"permits": server.runtime.Permits.List(eventID)})
}

func (server *Server) qualityExceptions(writer http.ResponseWriter, request *http.Request) {
	eventID := model.EventID(request.URL.Query().Get("event_id"))
	writeJSON(writer, http.StatusOK, map[string]any{"exceptions": server.runtime.Exceptions.List(eventID)})
}
