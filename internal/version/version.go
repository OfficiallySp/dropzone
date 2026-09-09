// Package version holds the application version, stamped at build time.
package version

// Current is the dropzone version. Override at build time with
// -ldflags "-X dropzone/internal/version.Current=x.y.z".
var Current = "1.0.1"
