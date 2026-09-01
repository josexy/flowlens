package pythonpluginservice

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxProtocolFrameBytes = 64 * 1024 * 1024

var errProtocolFrameTooLarge = errors.New("Python worker protocol frame exceeds the 64 MiB limit")

type workerMessage struct {
	Type            string          `json:"type"`
	RequestID       string          `json:"requestId,omitempty"`
	ExecutionID     string          `json:"executionId,omitempty"`
	PluginID        string          `json:"pluginId,omitempty"`
	Level           string          `json:"level,omitempty"`
	Stream          string          `json:"stream,omitempty"`
	Message         string          `json:"message,omitempty"`
	Timestamp       int64           `json:"timestamp,omitempty"`
	Code            string          `json:"code,omitempty"`
	Traceback       string          `json:"traceback,omitempty"`
	ProtocolVersion int             `json:"protocolVersion,omitempty"`
	SDKAPIVersion   int             `json:"sdkApiVersion,omitempty"`
	PythonVersion   []int           `json:"pythonVersion,omitempty"`
	Implementation  string          `json:"implementation,omitempty"`
	Validated       bool            `json:"validated,omitempty"`
	Blocked         bool            `json:"blocked,omitempty"`
	Transformed     bool            `json:"transformed,omitempty"`
	BodyChanged     bool            `json:"bodyChanged,omitempty"`
	Shutdown        bool            `json:"shutdown,omitempty"`
	Value           json.RawMessage `json:"value,omitempty"`
	Shared          json.RawMessage `json:"shared,omitempty"`
}

func writeProtocolFrame(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Python worker protocol frame: %w", err)
	}
	if len(payload) > maxProtocolFrameBytes {
		return errProtocolFrameTooLarge
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeFull(writer, header[:]); err != nil {
		return fmt.Errorf("write Python worker protocol frame header: %w", err)
	}
	if err := writeFull(writer, payload); err != nil {
		return fmt.Errorf("write Python worker protocol frame payload: %w", err)
	}
	return nil
}

func readProtocolFrame(reader io.Reader) (workerMessage, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return workerMessage{}, fmt.Errorf("read Python worker protocol frame header: %w", err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > maxProtocolFrameBytes {
		return workerMessage{}, errProtocolFrameTooLarge
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return workerMessage{}, fmt.Errorf("read Python worker protocol frame payload: %w", err)
	}
	var message workerMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return workerMessage{}, fmt.Errorf("decode Python worker protocol frame: %w", err)
	}
	if message.Type == "" {
		return workerMessage{}, errors.New("Python worker protocol frame is missing type")
	}
	return message, nil
}

func writeFull(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		count, err := writer.Write(value)
		if err != nil {
			return err
		}
		if count <= 0 {
			return io.ErrShortWrite
		}
		value = value[count:]
	}
	return nil
}
