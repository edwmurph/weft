package version

var Version = "10.1.1"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
