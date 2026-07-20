package context

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const managedAPIVersion = "upmctl.upm.io/v1alpha1"

type Trust string

const (
	TrustManagedValid   Trust = "MANAGED_VALID"
	TrustLegacyReadOnly Trust = "LEGACY_UNTRUSTED_READONLY"
	TrustInvalid        Trust = "INVALID"
	TrustUnknown        Trust = "UNKNOWN"
)

type Deployment struct {
	RepositoryRoot string            `json:"repositoryRoot,omitempty"`
	Workspace      string            `json:"workspace,omitempty"`
	Vagrantfile    string            `json:"vagrantfile,omitempty"`
	ConfigFile     string            `json:"configFile,omitempty"`
	VagrantData    string            `json:"vagrantData,omitempty"`
	Inventory      string            `json:"inventory,omitempty"`
	Kubeconfig     string            `json:"kubeconfig,omitempty"`
	StateFile      string            `json:"stateFile,omitempty"`
	EnvironmentID  string            `json:"environmentId,omitempty"`
	MachineIDs     map[string]string `json:"machineIds,omitempty"`
	Managed        bool              `json:"managed"`
	Trust          Trust             `json:"trust"`
	Source         string            `json:"source"`
	Findings       []string          `json:"findings,omitempty"`
}

func Discover(cwd, explicitWorkspace string) (Deployment, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return Deployment{}, fmt.Errorf("get current directory: %w", err)
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return Deployment{}, fmt.Errorf("resolve current directory: %w", err)
	}

	if explicitWorkspace != "" {
		workspace, err := filepath.Abs(explicitWorkspace)
		if err != nil {
			return Deployment{}, fmt.Errorf("resolve explicit workspace: %w", err)
		}
		if !isDirectory(workspace) {
			return Deployment{}, fmt.Errorf("explicit workspace is not a directory: %s", workspace)
		}
		if !isDeploymentWorkspace(workspace) {
			return Deployment{}, fmt.Errorf("explicit workspace does not contain Vagrantfile and vagrant/config.rb: %s", workspace)
		}
		return describe(findRepositoryRoot(workspace), workspace, "explicit"), nil
	}

	for current := cwd; ; current = filepath.Dir(current) {
		if isDeploymentWorkspace(current) {
			return describe(findRepositoryRoot(current), current, "ancestor"), nil
		}
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}

	repository := findRepositoryRoot(cwd)
	if repository != "" {
		standard := filepath.Join(repository, "vagrant_setup_scripts", "kubespray-upm")
		if isDeploymentWorkspace(standard) {
			return describe(repository, standard, "standard-nested"), nil
		}
		return Deployment{
			RepositoryRoot: repository,
			Managed:        false,
			Trust:          TrustUnknown,
			Source:         "repository-only",
			Findings:       []string{"standard deployment workspace was not found"},
		}, nil
	}

	return Deployment{}, fmt.Errorf("no kubespray-upm repository or deployment workspace found from %s", cwd)
}

func describe(repository, workspace, source string) Deployment {
	deployment := Deployment{
		RepositoryRoot: repository,
		Workspace:      workspace,
		Vagrantfile:    filepath.Join(workspace, "Vagrantfile"),
		ConfigFile:     filepath.Join(workspace, "vagrant", "config.rb"),
		VagrantData:    filepath.Join(workspace, ".vagrant"),
		Inventory:      filepath.Join(workspace, "inventory", "sample"),
		StateFile:      filepath.Join(workspace, ".upmctl", "state.json"),
		Source:         source,
		Trust:          TrustLegacyReadOnly,
	}

	for _, candidate := range []string{
		filepath.Join(workspace, "inventory", "sample", "artifacts", "admin.conf"),
		filepath.Join(workspace, "artifacts", "admin.conf"),
	} {
		if fileExists(candidate) {
			deployment.Kubeconfig = candidate
			break
		}
	}

	if !fileExists(deployment.ConfigFile) {
		deployment.Findings = append(deployment.Findings, "vagrant/config.rb is missing")
	}
	if !isDirectory(deployment.VagrantData) {
		deployment.Findings = append(deployment.Findings, ".vagrant metadata is missing")
	}
	state, managed, stateFinding := validateManagedState(workspace, deployment.StateFile)
	if managed {
		deployment.Managed = true
		deployment.Trust = TrustManagedValid
		deployment.EnvironmentID = state.EnvironmentID
		deployment.MachineIDs = state.Machines
	} else if fileExists(deployment.StateFile) {
		deployment.Trust = TrustInvalid
		deployment.Findings = append(deployment.Findings, stateFinding)
	}
	if !deployment.Managed && deployment.Trust == TrustLegacyReadOnly {
		deployment.Findings = append(deployment.Findings, "workspace is legacy read-only because .upmctl/state.json is missing")
	}
	return deployment
}

type managedState struct {
	APIVersion    string            `json:"apiVersion"`
	Kind          string            `json:"kind"`
	EnvironmentID string            `json:"environmentId"`
	Workspace     string            `json:"workspace"`
	Files         map[string]string `json:"files"`
	Machines      map[string]string `json:"machines,omitempty"`
	Adoption      managedAdoption   `json:"adoption"`
}

type managedAdoption struct {
	AdoptedAt     string               `json:"adoptedAt"`
	Actor         managedActor         `json:"actor"`
	HumanPresence managedHumanPresence `json:"humanPresence"`
	Reason        string               `json:"reason"`
	RequestID     string               `json:"requestId"`
	CLIVersion    string               `json:"cliVersion"`
}

type managedActor struct {
	Subject    string `json:"subject"`
	UID        string `json:"uid"`
	Username   string `json:"username"`
	Hostname   string `json:"hostname"`
	Source     string `json:"source"`
	AuthMethod string `json:"authMethod"`
}

type managedHumanPresence struct {
	Method          string `json:"method"`
	Terminal        string `json:"terminal"`
	ChallengeDigest string `json:"challengeDigest"`
	ConfirmedAt     string `json:"confirmedAt"`
}

var (
	managedMachinePattern = regexp.MustCompile(`^k8s-[1-8]$`)
	managedUUIDPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	managedEnvironmentID  = regexp.MustCompile(`^env-[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	managedDigestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func validateManagedState(workspace, stateFile string) (managedState, bool, string) {
	if !safeRegularFile(workspace, stateFile, 1<<20) {
		return managedState{}, false, "managed state is not a safe regular file"
	}
	stateInfo, stateErr := os.Lstat(stateFile)
	directoryInfo, directoryErr := os.Lstat(filepath.Dir(stateFile))
	if stateErr != nil || stateInfo.Mode().Perm() != 0o600 || directoryErr != nil || directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o700 {
		return managedState{}, false, "managed state must use a real 0700 directory and 0600 regular file"
	}
	contents, err := os.ReadFile(stateFile)
	if err != nil {
		return managedState{}, false, "managed state is missing"
	}
	var state managedState
	if err := rejectDuplicateJSONKeys(contents); err != nil {
		return managedState{}, false, "managed state contains duplicate JSON keys or invalid JSON"
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return managedState{}, false, "managed state is not valid JSON"
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return managedState{}, false, "managed state contains trailing JSON data"
	}
	if state.APIVersion != managedAPIVersion || state.Kind != "ManagedEnvironment" || !managedEnvironmentID.MatchString(state.EnvironmentID) {
		return managedState{}, false, "managed state identity or API version is invalid"
	}
	if _, err := time.Parse(time.RFC3339Nano, state.Adoption.AdoptedAt); err != nil || state.Adoption.AdoptedAt != state.Adoption.HumanPresence.ConfirmedAt ||
		state.Adoption.Actor.Source != "human-cli" || state.Adoption.Actor.AuthMethod != "interactive-tty" || state.Adoption.HumanPresence.Method != "typed-challenge" ||
		!managedDigestPattern.MatchString(state.Adoption.HumanPresence.ChallengeDigest) || strings.TrimSpace(state.Adoption.Actor.Subject) == "" ||
		strings.TrimSpace(state.Adoption.Actor.UID) == "" || strings.TrimSpace(state.Adoption.Actor.Username) == "" || strings.TrimSpace(state.Adoption.Actor.Hostname) == "" ||
		strings.TrimSpace(state.Adoption.HumanPresence.Terminal) == "" || strings.TrimSpace(state.Adoption.Reason) == "" || strings.TrimSpace(state.Adoption.RequestID) == "" || strings.TrimSpace(state.Adoption.CLIVersion) == "" {
		return managedState{}, false, "managed state adoption evidence is invalid"
	}
	canonicalWorkspace, err := canonicalPath(workspace)
	if err != nil {
		return managedState{}, false, "workspace canonical path cannot be resolved"
	}
	stateWorkspace, err := canonicalPath(state.Workspace)
	if err != nil || stateWorkspace != canonicalWorkspace {
		return managedState{}, false, "managed state workspace identity does not match"
	}
	requiredFiles := []string{"Vagrantfile", filepath.Join("vagrant", "config.rb")}
	for _, candidate := range []string{
		filepath.Join("inventory", "sample", "artifacts", "admin.conf"),
		filepath.Join("artifacts", "admin.conf"),
	} {
		if fileExists(filepath.Join(workspace, candidate)) {
			requiredFiles = append(requiredFiles, candidate)
			break
		}
	}
	for _, relative := range requiredFiles {
		if state.Files[filepath.ToSlash(relative)] == "" {
			return managedState{}, false, fmt.Sprintf("managed file identity is missing: %s", filepath.ToSlash(relative))
		}
	}
	seenUUIDs := map[string]string{}
	for name, uuid := range state.Machines {
		if !managedMachinePattern.MatchString(name) || !managedUUIDPattern.MatchString(uuid) {
			return managedState{}, false, "managed state contains an invalid machine identity"
		}
		normalizedUUID := strings.ToLower(uuid)
		if previous, duplicate := seenUUIDs[normalizedUUID]; duplicate && previous != name {
			return managedState{}, false, "managed state contains duplicate libvirt UUIDs"
		}
		seenUUIDs[normalizedUUID] = name
	}
	allowedFiles := map[string]bool{
		"Vagrantfile":                           true,
		"vagrant/config.rb":                     true,
		"inventory/sample/artifacts/admin.conf": true,
		"artifacts/admin.conf":                  true,
	}
	for path := range state.Files {
		if !allowedFiles[path] {
			return managedState{}, false, "managed state contains an unsupported file identity"
		}
		absolute := filepath.Join(workspace, filepath.FromSlash(path))
		if !safeRegularFile(workspace, absolute, 16<<20) {
			return managedState{}, false, fmt.Sprintf("managed file is not a safe regular file: %s", path)
		}
		actual, err := fileSHA256(absolute)
		if err != nil || state.Files[path] != "sha256:"+actual {
			return managedState{}, false, fmt.Sprintf("managed file digest does not match: %s", path)
		}
	}
	return state, true, ""
}

func rejectDuplicateJSONKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate or invalid object key")
			}
			seen[key] = true
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON token")
		}
		return err
	}
	return nil
}

func safeRegularFile(workspace, path string, maxSize int64) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxSize {
		return false
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	root = filepath.Clean(root)
	resolved = filepath.Clean(resolved)
	return resolved == root || strings.HasPrefix(resolved, root+string(os.PathSeparator))
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func fileSHA256(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:]), nil
}

func isDeploymentWorkspace(path string) bool {
	return fileExists(filepath.Join(path, "Vagrantfile")) && fileExists(filepath.Join(path, "vagrant", "config.rb"))
}

func findRepositoryRoot(start string) string {
	for current := start; ; current = filepath.Dir(current) {
		if fileExists(filepath.Join(current, "vagrant_setup_scripts", "libvirt_kubespray_setup.sh")) &&
			fileExists(filepath.Join(current, "playbooks", "cluster.yml")) {
			return current
		}
		if parent := filepath.Dir(current); parent == current {
			return ""
		}
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
