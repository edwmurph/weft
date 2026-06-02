package version

var Version = "0.12.5"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
