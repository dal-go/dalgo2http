// Package dalgo2http exposes read-only HTTP/JSON endpoints as DALgo collections.
//
// The adapter is declarative and fails closed: a query runs only when every
// condition can be pushed to the endpoint. See README.md for the constraints.
package dalgo2http
