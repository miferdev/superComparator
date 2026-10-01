// Package version lleva la versión y el commit con los que se compiló el
// programa. La build los inyecta con -ldflags para que los informes digan con
// qué código se generaron.
package version

import (
	"fmt"
	"runtime/debug"
)

var (
	// Version es la versión del programa (0.3.0-dev por defecto).
	Version = "0.4.0"
	// Commit es el hash corto del commit; "dev" si se compiló sin ldflags.
	Commit = "dev"
	// Matching es la versión de las reglas de coincidencia. Cuando cambian las
	// reglas, los matches guardados dejan de ser válidos, así que se vuelve a
	// resolver la lista entera en lugar de fiarse de ellos.
	Matching = "0.4.0"
)

// Stamp es la firma que se escribe en los informes.
func Stamp() string {
	if Commit == "" || Commit == "dev" {
		return "supercomparator " + Version
	}
	return fmt.Sprintf("supercomparator %s · commit %s", Version, Commit)
}

// Short devuelve la versión sin el commit, para el comando version.
func Short() string { return "supercomparator " + Version }

// DeVCS devuelve el commit que Gitgrabó el binario, si está disponible.
func DeVCS() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Commit
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return s.Value[:7]
		}
	}
	return Commit
}
