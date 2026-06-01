package version

var Version = "7.16.5"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
