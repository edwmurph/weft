package version

var Version = "0.12.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
