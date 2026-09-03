package admin

import (
	"net/http"
	"strings"

	"github.com/open-carrier-network/ocn/internal/ksim"
	"github.com/open-carrier-network/ocn/internal/registry"
	"github.com/open-carrier-network/ocn/internal/store"
)

// handleFederationStatus reports the panel-saved federation settings plus the
// currently assigned area code (from this server's live state/config).
func (s *Server) handleFederationStatus(w http.ResponseWriter, r *http.Request) {
	fs, err := s.store.GetFederationSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	area, _ := s.store.GetSetting("area_code")
	liveArea := area
	if s.opts.Area != nil {
		if a := s.opts.Area(); a != "" {
			liveArea = a
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"configured":                fs.RegistryAddress != "",
		"registry_address":          fs.RegistryAddress,
		"registry_insecure":         fs.RegistryInsecure,
		"requested_area_code":       fs.RequestedAreaCode,
		"federation_public_address": fs.FederationPublicAddr,
		"area_code":                 area, // "" when not yet assigned
		"server_area_code":          liveArea,
	})
}

// handleFederationRegister saves the settings and registers this server with
// the registry using its own key. Activation takes effect on restart.
func (s *Server) handleFederationRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RegistryAddress      string `json:"registry_address"`
		RegistryInsecure     bool   `json:"registry_insecure"`
		RequestedAreaCode    string `json:"requested_area_code"`
		FederationPublicAddr string `json:"federation_public_address"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	req.RegistryAddress = strings.TrimSpace(req.RegistryAddress)
	if req.RegistryAddress == "" {
		writeErr(w, http.StatusBadRequest, "registry_address is required")
		return
	}
	req.RequestedAreaCode = strings.TrimSpace(req.RequestedAreaCode)
	req.FederationPublicAddr = strings.TrimSpace(req.FederationPublicAddr)
	if req.FederationPublicAddr == "" {
		writeErr(w, http.StatusBadRequest, "federation_public_address is required (other servers reach you on it)")
		return
	}

	key, _, err := ksim.LoadFile(s.opts.ServerKeyPath, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot load server key: "+err.Error())
		return
	}

	regClient, err := registry.Dial(req.RegistryAddress, req.RegistryInsecure)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cannot reach registry: "+err.Error())
		return
	}
	defer regClient.Close()
	regClient.SetIdentity(key)

	area, err := regClient.RegisterServer(
		s.opts.ServerName, "registered from admin panel", req.FederationPublicAddr, req.RequestedAreaCode, key.PublicKey,
	)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "registry refused registration: "+err.Error())
		return
	}

	fs := &store.FederationSettings{
		RegistryAddress:      req.RegistryAddress,
		RegistryInsecure:     req.RegistryInsecure,
		RequestedAreaCode:    req.RequestedAreaCode,
		FederationPublicAddr: req.FederationPublicAddr,
	}
	if err := s.store.SaveFederationSettings(fs); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetSetting("area_code", area); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Hot-join: apply federation to the running server without a restart.
	if s.opts.OnFederated != nil {
		if err := s.opts.OnFederated(fs, area); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":               true,
				"area_code":        area,
				"restart_required": true,
				"message":          "Registered with area code " + area + " but live activation failed (" + err.Error() + "). Restart to apply.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":               true,
			"area_code":        area,
			"restart_required": false,
			"message":          "Registered and federated with area code " + area + ". Active now — no restart needed.",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":               true,
		"area_code":        area,
		"restart_required": true,
		"message":          "Registered. Restart the OCN server to activate federation.",
	})
}

// handleFederationClear disables panel-configured federation on next restart.
func (s *Server) handleFederationClear(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearFederationSettings(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":               true,
		"restart_required": true,
		"message":          "Federation settings cleared. Restart to run standalone.",
	})
}
