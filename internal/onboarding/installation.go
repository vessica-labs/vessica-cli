package onboarding

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

type Installation struct {
	WorkspaceID   string        `json:"railway_workspace_id"`
	ProjectID     string        `json:"railway_project_id"`
	Endpoint      string        `json:"endpoint"`
	CredentialRef string        `json:"credential_ref"`
	Config        config.Config `json:"config"`
}

func installationsPath() (string, error) {
	return RegistryPath()
}

func loadInstallations() ([]Installation, error) {
	db, err := openClientDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT workspace_id,project_id,endpoint,credential_ref,config_json FROM installations ORDER BY updated_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var installations []Installation
	for rows.Next() {
		var installation Installation
		var configJSON string
		if err := rows.Scan(&installation.WorkspaceID, &installation.ProjectID, &installation.Endpoint, &installation.CredentialRef, &configJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(configJSON), &installation.Config); err != nil {
			return nil, err
		}
		installations = append(installations, installation)
	}
	return installations, rows.Err()
}

func SaveInstallation(cfg config.Config, credentials []byte) error {
	if cfg.Hosted.WorkspaceID == "" || cfg.Hosted.ProjectID == "" || cfg.Hosted.ControlPlaneURL == "" {
		return fmt.Errorf("complete hosted installation identity is required")
	}
	reference := "installation-" + strings.ToLower(cfg.Hosted.ProjectID)
	if err := auth.StoreSecret(reference, credentials); err != nil {
		return err
	}
	next := Installation{WorkspaceID: cfg.Hosted.WorkspaceID, ProjectID: cfg.Hosted.ProjectID, Endpoint: cfg.Hosted.ControlPlaneURL, CredentialRef: reference, Config: cfg}
	configJSON, err := json.Marshal(next.Config)
	if err != nil {
		return err
	}
	db, err := openClientDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO installations(project_id,workspace_id,endpoint,credential_ref,config_json,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET workspace_id=excluded.workspace_id,endpoint=excluded.endpoint,credential_ref=excluded.credential_ref,config_json=excluded.config_json,updated_at=excluded.updated_at`, next.ProjectID, next.WorkspaceID, next.Endpoint, next.CredentialRef, string(configJSON), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// RemoveInstallation forgets a hosted installation from this client's registry
// and credential store. It does not mutate the Railway project.
func RemoveInstallation(projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	db, err := openClientDB()
	if err != nil {
		return err
	}
	defer db.Close()
	var credentialRef string
	err = db.QueryRow(`SELECT credential_ref FROM installations WHERE project_id=?`, projectID).Scan(&credentialRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := db.Exec(`DELETE FROM installations WHERE project_id=?`, projectID); err != nil {
		return err
	}
	return auth.DeleteSecret(credentialRef)
}

func FindInstallation(workspace string) (*Installation, []byte, error) {
	installations, err := loadInstallations()
	if err != nil {
		return nil, nil, err
	}
	var matches []Installation
	for _, installation := range installations {
		if workspace == "" || strings.EqualFold(workspace, installation.WorkspaceID) {
			matches = append(matches, installation)
		}
	}
	if len(matches) == 0 {
		return nil, nil, os.ErrNotExist
	}
	if len(matches) > 1 {
		return nil, nil, fmt.Errorf("multiple Vessica installations are known; select a Railway workspace")
	}
	credentials, err := auth.LoadSecret(matches[0].CredentialRef)
	if err != nil {
		return nil, nil, err
	}
	return &matches[0], credentials, nil
}
