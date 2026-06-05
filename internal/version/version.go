package version

var Version = "0.18.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
