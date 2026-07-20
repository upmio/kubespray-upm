package buildinfo

import "runtime"

var (
	Version   = "0.1.0-dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

const APIVersion = "upmctl.upm.io/v1alpha1"

type Info struct {
	Version    string `json:"version"`
	GitCommit  string `json:"gitCommit"`
	BuildDate  string `json:"buildDate"`
	GoVersion  string `json:"goVersion"`
	Platform   string `json:"platform"`
	APIVersion string `json:"apiVersion"`
}

func Current() Info {
	return Info{
		Version:    Version,
		GitCommit:  GitCommit,
		BuildDate:  BuildDate,
		GoVersion:  runtime.Version(),
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		APIVersion: APIVersion,
	}
}
