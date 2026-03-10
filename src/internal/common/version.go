package common

type jsonVersion struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	BuildSig  string `json:"build_sig"`
}

var JSONVersion jsonVersion
