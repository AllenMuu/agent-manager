package memoryintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memoryprovider"
	"io"
	"os"
	"strings"
)

// AccessConfig is trusted operator configuration. ClientInfo and tool arguments
// cannot broaden its exact owner, writer authority or provider selection.
type AccessConfig struct {
	SessionConfig
	Provider memory.ProviderConfig `json:"provider"`
}

func LoadAccess(path string) (AccessConfig, error) {
	data, err := readRegular(path, 64*1024)
	if err != nil {
		return AccessConfig{}, err
	}
	var c AccessConfig
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if memory.ValidateCanonicalJSON(data) != nil || d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, memory.ErrInvalidInput
	}
	if c.Agent != "codex" && c.Agent != "claude-code" || c.Owner.Kind != memory.OwnerProject || memory.ValidateOwner(c.Owner) != nil || c.Provider.Validate() != nil {
		return c, memory.ErrInvalidInput
	}
	hasProject := false
	for _, scope := range c.Provider.Scopes {
		if scope == memory.ScopeProject {
			hasProject = true
		}
	}
	if !hasProject {
		return c, memory.ErrUnsupported
	}
	return c, nil
}
func (c AccessConfig) Gateway() (*memory.Gateway, error) {
	provider, err := memoryprovider.OpenWithSecrets(c.Provider, fileSecrets{})
	if err != nil {
		return nil, memory.SafeError(err)
	}
	access := memory.Access{ReadOwners: []memory.Owner{c.Owner}, AllowWeakWrites: c.AllowWeakWrites}
	if c.AllowWeakWrites {
		access.WriteOwners = []memory.Owner{c.Owner}
	}
	return memory.NewGateway(provider, access, c.Provider.Retrieval)
}

// File credentials are read inside the transport process, never inserted in
// native agent environments or access snippets. No raw resolver errors escape.
type fileSecrets struct{}

func (fileSecrets) Resolve(ctx context.Context, ref memory.ConfigReference) (string, error) {
	if ctx == nil || ctx.Err() != nil {
		return "", memory.ErrCanceled
	}
	if ref.Kind != "file" {
		return "", memory.ErrUnsupported
	}
	data, err := readRegular(ref.Name, 8192)
	if err != nil {
		return "", memory.ErrAuthentication
	}
	if ctx.Err() != nil {
		return "", memory.ErrCanceled
	}
	value := strings.TrimSuffix(string(data), "\n")
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", memory.ErrAuthentication
	}
	return value, nil
}
func readRegular(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, memory.ErrInvalidInput
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, memory.ErrInvalidInput
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, memory.ErrInvalidInput
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, memory.ErrInvalidInput
	}
	return data, nil
}
