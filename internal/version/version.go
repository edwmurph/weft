package version

var Version = "0.17.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
