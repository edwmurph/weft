package version

var Version = "7.17.4"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
