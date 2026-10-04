// Package benchmark measures how fast a model runs on this machine and
// describes the result as a report that anyone can share.
//
// `self benchmark <model>` starts the engine, sends a fixed set of requests
// (see Scenarios) and writes a Report as JSON. The report holds what is needed
// to compare it with others: the model files and their sha256, the hardware,
// the version of the requests, and the timings and token rates.
package benchmark

// Version identifies the scenarios and their request texts. Bump it whenever
// either changes: results of different versions are not comparable.
const Version = 1

// SchemaVersion is the layout version of a Report.
const SchemaVersion = 2
