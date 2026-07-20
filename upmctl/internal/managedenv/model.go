package managedenv

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	APIVersion = "upmctl.upm.io/v1alpha1"
	Kind       = "ManagedEnvironment"
)

var (
	environmentIDPattern = regexp.MustCompile(`^env-[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	machineNamePattern   = regexp.MustCompile(`^k8s-[1-8]$`)
	uuidPattern          = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	digestPattern        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// State is the immutable local identity created when a legacy libvirt
// workspace is adopted. It contains only declarations and content digests;
// adoption does not execute or mutate Vagrant, libvirt, or Kubernetes.
type State struct {
	APIVersion    string            `json:"apiVersion"`
	Kind          string            `json:"kind"`
	EnvironmentID string            `json:"environmentId"`
	Workspace     string            `json:"workspace"`
	Files         map[string]string `json:"files"`
	Machines      map[string]string `json:"machines"`
	Adoption      Adoption          `json:"adoption"`
}

type Actor struct {
	Subject    string `json:"subject"`
	UID        string `json:"uid"`
	Username   string `json:"username"`
	Hostname   string `json:"hostname"`
	Source     string `json:"source"`
	AuthMethod string `json:"authMethod"`
}

type HumanPresence struct {
	Method          string `json:"method"`
	Terminal        string `json:"terminal"`
	ChallengeDigest string `json:"challengeDigest"`
	ConfirmedAt     string `json:"confirmedAt"`
}

type Adoption struct {
	AdoptedAt     string        `json:"adoptedAt"`
	Actor         Actor         `json:"actor"`
	HumanPresence HumanPresence `json:"humanPresence"`
	Reason        string        `json:"reason"`
	RequestID     string        `json:"requestId"`
	CLIVersion    string        `json:"cliVersion"`
}

type ActorObservation struct {
	Subject  string
	UID      string
	Username string
	Hostname string
}

type PresenceObservation struct {
	Terminal        string
	ChallengeDigest string
}

func ValidEnvironmentID(value string) bool {
	return environmentIDPattern.MatchString(value)
}

func (s State) Validate() error {
	if err := s.validateSnapshot(); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, s.Adoption.AdoptedAt); err != nil {
		return fmt.Errorf("adoption adoptedAt is invalid")
	}
	for name, value := range map[string]string{
		"actor subject": s.Adoption.Actor.Subject, "actor uid": s.Adoption.Actor.UID,
		"actor username": s.Adoption.Actor.Username, "actor hostname": s.Adoption.Actor.Hostname,
		"reason": s.Adoption.Reason, "requestId": s.Adoption.RequestID, "cliVersion": s.Adoption.CLIVersion,
		"terminal": s.Adoption.HumanPresence.Terminal,
	} {
		if err := validateText(name, value, 1024); err != nil {
			return err
		}
	}
	if s.Adoption.Actor.Source != "human-cli" || s.Adoption.Actor.AuthMethod != "interactive-tty" || s.Adoption.HumanPresence.Method != "typed-challenge" {
		return fmt.Errorf("adoption human boundary labels are invalid")
	}
	if !digestPattern.MatchString(s.Adoption.HumanPresence.ChallengeDigest) || s.Adoption.HumanPresence.ConfirmedAt != s.Adoption.AdoptedAt {
		return fmt.Errorf("adoption human presence evidence is invalid")
	}
	return nil
}

func (s State) validateSnapshot() error {
	if s.APIVersion != APIVersion || s.Kind != Kind {
		return fmt.Errorf("invalid managed environment API identity")
	}
	if !ValidEnvironmentID(s.EnvironmentID) {
		return fmt.Errorf("environment ID must match env-<lowercase letters, digits, and internal hyphens>")
	}
	if strings.TrimSpace(s.Workspace) == "" {
		return fmt.Errorf("workspace is required")
	}
	for _, required := range []string{"Vagrantfile", "vagrant/config.rb"} {
		if !digestPattern.MatchString(s.Files[required]) {
			return fmt.Errorf("required file digest is missing or invalid: %s", required)
		}
	}
	allowedFiles := map[string]bool{
		"Vagrantfile":                           true,
		"vagrant/config.rb":                     true,
		"inventory/sample/artifacts/admin.conf": true,
		"artifacts/admin.conf":                  true,
	}
	for path, digest := range s.Files {
		if !allowedFiles[path] || !digestPattern.MatchString(digest) {
			return fmt.Errorf("unsupported or invalid managed file identity: %s", path)
		}
	}
	if len(s.Machines) == 0 {
		return fmt.Errorf("at least one machine identity is required")
	}
	seen := map[string]string{}
	for name, uuid := range s.Machines {
		if !machineNamePattern.MatchString(name) || !uuidPattern.MatchString(uuid) {
			return fmt.Errorf("invalid machine identity: %s", name)
		}
		if previous, exists := seen[uuid]; exists && previous != name {
			return fmt.Errorf("duplicate libvirt UUID for %s and %s", previous, name)
		}
		seen[uuid] = name
	}
	return nil
}

func BindAdoption(state State, actor ActorObservation, presence PresenceObservation, reason, requestID, cliVersion string, now time.Time) (State, error) {
	if err := state.validateSnapshot(); err != nil {
		return State{}, err
	}
	if now.IsZero() {
		return State{}, fmt.Errorf("adoption time is required")
	}
	for name, value := range map[string]string{
		"actor subject": actor.Subject, "actor uid": actor.UID, "actor username": actor.Username,
		"actor hostname": actor.Hostname, "terminal": presence.Terminal, "reason": reason,
		"requestId": requestID, "cliVersion": cliVersion,
	} {
		if err := validateText(name, value, 1024); err != nil {
			return State{}, err
		}
	}
	if !digestPattern.MatchString(presence.ChallengeDigest) {
		return State{}, fmt.Errorf("challengeDigest is invalid")
	}
	when := now.UTC().Format(time.RFC3339Nano)
	state.Adoption = Adoption{
		AdoptedAt:     when,
		Actor:         Actor{Subject: actor.Subject, UID: actor.UID, Username: actor.Username, Hostname: actor.Hostname, Source: "human-cli", AuthMethod: "interactive-tty"},
		HumanPresence: HumanPresence{Method: "typed-challenge", Terminal: presence.Terminal, ChallengeDigest: presence.ChallengeDigest, ConfirmedAt: when},
		Reason:        reason, RequestID: requestID, CLIVersion: cliVersion,
	}
	return state, state.Validate()
}

func validateText(name, value string, maximum int) error {
	if strings.TrimSpace(value) == "" || len(value) > maximum {
		return fmt.Errorf("%s is required and must not exceed %d bytes", name, maximum)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s contains control characters", name)
		}
	}
	return nil
}

func SortedMachineNames(machines map[string]string) []string {
	names := make([]string, 0, len(machines))
	for name := range machines {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func SortedFileNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
