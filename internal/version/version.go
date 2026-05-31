package version

var Version = "7.5.4"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
