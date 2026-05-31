package version

var Version = "7.15.5"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
