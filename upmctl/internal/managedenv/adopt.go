package managedenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	upmconfig "github.com/upmio/kubespray-upm/upmctl/internal/config"
)

type FailureCode string

const (
	FailureInvalidEnvironmentID FailureCode = "INVALID_ENVIRONMENT_ID"
	FailureUnsafeWorkspace      FailureCode = "UNSAFE_WORKSPACE"
	FailureAlreadyControlled    FailureCode = "CONTROL_STATE_EXISTS"
	FailureConfigInvalid        FailureCode = "CONFIG_INVALID"
	FailureUnsupportedProvider  FailureCode = "UNSUPPORTED_PROVIDER"
	FailureMetadataInvalid      FailureCode = "METADATA_INVALID"
	FailureStoreUnsafe          FailureCode = "STORE_UNSAFE"
	FailureStateExists          FailureCode = "STATE_EXISTS"
)

type Failure struct {
	Code   FailureCode
	Reason string
}

func (e *Failure) Error() string { return e.Reason }

func failure(code FailureCode, format string, arguments ...any) error {
	return &Failure{Code: code, Reason: fmt.Sprintf(format, arguments...)}
}

func FailureOf(err error) *Failure {
	var value *Failure
	if errors.As(err, &value) {
		return value
	}
	return &Failure{Code: FailureStoreUnsafe, Reason: err.Error()}
}

// Prepare validates and snapshots a legacy workspace without executing any
// external command. Save must be called separately to publish state.json.
func Prepare(workspace, environmentID string) (State, error) {
	if !ValidEnvironmentID(environmentID) {
		return State{}, failure(FailureInvalidEnvironmentID, "environment ID %q must match env-<lowercase letters, digits, and internal hyphens>", environmentID)
	}
	root, err := realWorkspace(workspace)
	if err != nil {
		return State{}, failure(FailureUnsafeWorkspace, "%v", err)
	}
	if err := requireEmptyControlState(root); err != nil {
		return State{}, err
	}

	vagrantDigest, err := safeFileDigest(root, filepath.Join(root, "Vagrantfile"), 16<<20)
	if err != nil {
		return State{}, failure(FailureUnsafeWorkspace, "Vagrantfile is not a safe regular file: %v", err)
	}
	configPath := filepath.Join(root, "vagrant", "config.rb")
	configDigest, err := safeFileDigest(root, configPath, 1<<20)
	if err != nil {
		return State{}, failure(FailureConfigInvalid, "config.rb is not a safe regular file: %v", err)
	}
	validation := upmconfig.ParseFile(configPath)
	if !validation.Safe || !validation.Valid || !validation.Complete || validation.Digest != configDigest {
		return State{}, failure(FailureConfigInvalid, "config.rb must be safe, complete, valid, and stable (status=%s)", validation.Status)
	}

	files := map[string]string{
		"Vagrantfile":       vagrantDigest,
		"vagrant/config.rb": configDigest,
	}
	for _, relative := range []string{
		"inventory/sample/artifacts/admin.conf",
		"artifacts/admin.conf",
	} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		_, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return State{}, failure(FailureUnsafeWorkspace, "inspect kubeconfig %s: %v", relative, statErr)
		}
		digest, digestErr := safeFileDigest(root, path, 16<<20)
		if digestErr != nil {
			return State{}, failure(FailureUnsafeWorkspace, "kubeconfig %s is not a safe regular file: %v", relative, digestErr)
		}
		files[relative] = digest
	}

	machines, err := inspectLibvirtMetadata(root, validation.Config)
	if err != nil {
		return State{}, err
	}
	state := State{
		APIVersion: APIVersion, Kind: Kind, EnvironmentID: environmentID,
		Workspace: root, Files: files, Machines: machines,
	}
	if err := state.validateSnapshot(); err != nil {
		return State{}, failure(FailureMetadataInvalid, "%v", err)
	}
	return state, nil
}

func inspectLibvirtMetadata(root string, config upmconfig.Config) (map[string]string, error) {
	machinesPath := filepath.Join(root, ".vagrant", "machines")
	entries, err := readRealDirectory(root, machinesPath)
	if err != nil {
		return nil, failure(FailureMetadataInvalid, ".vagrant/machines is unavailable or unsafe: %v", err)
	}
	expected := map[string]bool{}
	for _, node := range config.ExpectedNodes() {
		expected[node.Name] = true
	}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			return nil, failure(FailureMetadataInvalid, "unknown Vagrant machine metadata %q", entry.Name())
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return nil, failure(FailureMetadataInvalid, "machine metadata %q is not a real directory", entry.Name())
		}
	}

	machines := make(map[string]string, len(expected))
	seenUUIDs := map[string]string{}
	names := make([]string, 0, len(expected))
	for name := range expected {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		nodePath := filepath.Join(machinesPath, name)
		providers, providerErr := readRealDirectory(root, nodePath)
		if providerErr != nil {
			return nil, failure(FailureMetadataInvalid, "machine metadata %q is missing or unsafe: %v", name, providerErr)
		}
		if len(providers) != 1 || providers[0].Name() != "libvirt" || providers[0].Type()&os.ModeSymlink != 0 || !providers[0].IsDir() {
			providerNames := make([]string, 0, len(providers))
			for _, provider := range providers {
				providerNames = append(providerNames, provider.Name())
			}
			return nil, failure(FailureUnsupportedProvider, "machine %s must have only libvirt metadata; found %v", name, providerNames)
		}
		idPath := filepath.Join(nodePath, "libvirt", "id")
		raw, readErr := safeFileRead(root, idPath, 256)
		if readErr != nil {
			return nil, failure(FailureMetadataInvalid, "machine %s has missing or unsafe libvirt id: %v", name, readErr)
		}
		if strings.ContainsRune(string(raw), '\x00') || strings.Count(strings.TrimSuffix(string(raw), "\n"), "\n") != 0 {
			return nil, failure(FailureMetadataInvalid, "machine %s libvirt id must contain exactly one UUID", name)
		}
		uuid := strings.ToLower(strings.TrimSpace(string(raw)))
		if !uuidPattern.MatchString(uuid) {
			return nil, failure(FailureMetadataInvalid, "machine %s has invalid libvirt UUID", name)
		}
		if previous, duplicate := seenUUIDs[uuid]; duplicate {
			return nil, failure(FailureMetadataInvalid, "machines %s and %s have duplicate libvirt UUID %s", previous, name, uuid)
		}
		seenUUIDs[uuid] = name
		machines[name] = uuid
	}
	return machines, nil
}

func requireEmptyControlState(root string) error {
	path := filepath.Join(root, ".upmctl")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return failure(FailureStoreUnsafe, "inspect .upmctl: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return failure(FailureStoreUnsafe, ".upmctl must be absent or an empty private real directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return failure(FailureStoreUnsafe, "read .upmctl: %v", err)
	}
	if len(entries) != 0 {
		return failure(FailureAlreadyControlled, "workspace already contains upmctl state or control-state")
	}
	if info.Mode().Perm() != 0o700 {
		return failure(FailureStoreUnsafe, "existing empty .upmctl directory must have mode 0700")
	}
	return nil
}

func realWorkspace(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("workspace is required")
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Lstat(absolute)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("workspace must be a real directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace identity: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func readRealDirectory(root, path string) ([]os.DirEntry, error) {
	if err := ensureWithin(root, path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("not a real directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(path) {
		return nil, fmt.Errorf("directory path traverses a symlink")
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	opened, err := directory.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("directory identity changed while opening")
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, current) {
		return nil, fmt.Errorf("directory identity changed while reading")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func safeFileDigest(root, path string, maxSize int64) (string, error) {
	contents, err := safeFileRead(root, path, maxSize)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func safeFileRead(root, path string, maxSize int64) ([]byte, error) {
	if err := ensureWithin(root, path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxSize {
		return nil, fmt.Errorf("not a bounded regular non-symlink file")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(path) {
		return nil, fmt.Errorf("file path traverses a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("file identity changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil || int64(len(contents)) > maxSize {
		return nil, fmt.Errorf("file read failed or exceeded maximum size")
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, current) {
		return nil, fmt.Errorf("file identity changed while reading")
	}
	return contents, nil
}

func ensureWithin(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return fmt.Errorf("path escapes workspace")
	}
	return nil
}
