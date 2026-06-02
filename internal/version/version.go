package version

var Version = "0.5.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
