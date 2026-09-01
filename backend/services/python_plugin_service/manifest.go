package pythonpluginservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	pluginSchemaVersion = 1
	pluginAPIVersion    = 1

	manifestFileName = "manifest.json"
	mainFileName     = "main.py"
	helpersFileName  = "helpers.py"
)

type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	APIVersion    int    `json:"apiVersion"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
}

func parseManifest(value []byte, expectedID string) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode plugin manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Manifest{}, errors.New("plugin manifest contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("decode plugin manifest: %w", err)
	}
	if err := validateManifest(&manifest, expectedID); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateManifest(manifest *Manifest, expectedID string) error {
	if manifest == nil {
		return errors.New("plugin manifest cannot be nil")
	}
	if manifest.SchemaVersion != pluginSchemaVersion {
		return fmt.Errorf("unsupported plugin manifest schema version %d", manifest.SchemaVersion)
	}
	if manifest.APIVersion != pluginAPIVersion {
		return fmt.Errorf("unsupported plugin API version %d", manifest.APIVersion)
	}
	manifest.ID = strings.TrimSpace(manifest.ID)
	if err := validateUUID(manifest.ID); err != nil {
		return fmt.Errorf("invalid plugin manifest ID: %w", err)
	}
	if expectedID = strings.TrimSpace(expectedID); expectedID != "" && manifest.ID != expectedID {
		return fmt.Errorf("plugin manifest ID %q does not match package ID %q", manifest.ID, expectedID)
	}
	name, description, _, err := normalizePluginInput(manifest.Name, manifest.Description, `{}`)
	if err != nil {
		return fmt.Errorf("invalid plugin manifest: %w", err)
	}
	manifest.Name = name
	manifest.Description = description
	return nil
}

func marshalManifest(manifest Manifest) ([]byte, error) {
	if err := validateManifest(&manifest, manifest.ID); err != nil {
		return nil, err
	}
	value, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode plugin manifest: %w", err)
	}
	return append(value, '\n'), nil
}

func defaultManifest(id, name, description string) (Manifest, error) {
	manifest := Manifest{
		SchemaVersion: pluginSchemaVersion,
		APIVersion:    pluginAPIVersion,
		ID:            id,
		Name:          name,
		Description:   description,
	}
	if err := validateManifest(&manifest, id); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

const defaultMainSource = `from flowlens import *

def onRequest(context, request):
    return request

def onResponse(context, response):
    return response
`

const defaultHelpersSource = `"""Shared helpers for this FlowLens plugin."""
`
