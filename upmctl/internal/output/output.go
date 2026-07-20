package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/buildinfo"
)

type Format string

const (
	Text  Format = "text"
	JSON  Format = "json"
	JSONL Format = "jsonl"
)

type Envelope struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	RequestID  string    `json:"requestId"`
	Timestamp  time.Time `json:"timestamp"`
	Data       any       `json:"data"`
}

type ErrorBody struct {
	Code        string         `json:"code"`
	Message     string         `json:"message"`
	Details     map[string]any `json:"details"`
	Remediation string         `json:"remediation"`
}

type ErrorEnvelope struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	RequestID  string    `json:"requestId"`
	Timestamp  time.Time `json:"timestamp"`
	Error      ErrorBody `json:"error"`
}

func WriteEnvelope(writer io.Writer, format Format, envelope Envelope) error {
	envelope.APIVersion = buildinfo.APIVersion
	if format == JSONL {
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(envelope)
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}

func WriteError(writer io.Writer, format Format, envelope ErrorEnvelope) error {
	envelope.APIVersion = buildinfo.APIVersion
	if envelope.Error.Details == nil {
		envelope.Error.Details = map[string]any{}
	}
	if format == Text {
		_, err := fmt.Fprintf(writer, "Error [%s]: %s\n", envelope.Error.Code, envelope.Error.Message)
		if err == nil && envelope.Error.Remediation != "" {
			_, err = fmt.Fprintf(writer, "Remediation: %s\n", envelope.Error.Remediation)
		}
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if format == JSON {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(envelope)
}
