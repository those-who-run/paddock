package main

// The known differences in what is spoken (paddock#64 R3): the one reviewed list of
// the places radio may say something other than v1 on the same recording, each
// with the decision behind it and the issue that took it. A difference no entry
// matches fails `parity spoken`, and so does an entry whose recording was compared
// and that matched nothing: once v2 says what v1 said, the entry goes.
//
// A real difference in spoken text is a bug in radio or ecu, to be fixed there, not
// listed here. Only a difference a decision already accepts elsewhere belongs here.

// knownDifference is one: on Recording (its stem), a callout of Rule that v1 said
// as V1 and radio as V2, "" for the side that said nothing in its place.
type knownDifference struct {
	ID        string
	Issue     string
	Decision  string
	Recording string
	Rule      string
	V1, V2    string
}

var knownSpoken = []knownDifference{}
