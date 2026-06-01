package version

var Version = "0.2.9"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
