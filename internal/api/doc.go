// Package api contains HTTP infrastructure shared by all model types.
//
// Type-specific handlers live in child packages such as api/decision and
// api/image. The shared router mounts one type-specific Mode at a time.
package api
