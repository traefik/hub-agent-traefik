/*
Copyright (C) 2022 Traefik Labs

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

package version

import (
	"fmt"
	"html/template"
	"io"
	"runtime"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

// These variables are set when compiling the project.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

var versionTemplate = `Version:      {{.Version}}
Commit:       {{ .Commit }}
Go version:   {{.GoVersion}}
Built:        {{.BuildTime}}
OS/Arch:      {{.Os}}/{{.Arch}}
`

// Print prints the full version information on the given writer.
func Print(w io.Writer) error {
	tmpl, err := template.New("").Parse(versionTemplate)
	if err != nil {
		return err
	}

	v := struct {
		Version   string
		Commit    string
		BuildTime string
		GoVersion string
		Os        string
		Arch      string
	}{
		Version:   version,
		Commit:    commit,
		BuildTime: date,
		GoVersion: runtime.Version(),
		Os:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	return tmpl.Execute(w, v)
}

// String returns a quick summary of version information.
func String() string {
	return fmt.Sprintf("%s, build %s on %s", version, commit, date)
}

// Version returns the agent version.
func Version() string {
	return version
}

// Log logs the full version information.
func Log() {
	log.Info().
		Str("version", version).
		Str("module", moduleName()).
		Str("commit", commit).
		Str("built", date).
		Str("go_version", runtime.Version()).
		Str("os", runtime.GOOS).
		Str("arch", runtime.GOARCH).
		Send()
}

func moduleName() string {
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		return buildInfo.Main.Path
	}
	return ""
}
