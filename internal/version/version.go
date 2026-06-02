package version

var Version = "0.12.4"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
