package version

var Version = "7.9.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
