package version

var Version = "0.18.9"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
