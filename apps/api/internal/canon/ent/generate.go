package ent

// entc's Go API instead of entgo.io/ent/cmd/ent: the CLI pulls a tablewriter
// whose displaywidth does not build against this module's uax29 graphemes.
//go:generate go run -mod=mod entc.go
