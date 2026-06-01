package version

var Version = "8.1.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
