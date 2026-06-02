package version

var Version = "0.7.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
