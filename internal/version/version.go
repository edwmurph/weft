package version

var Version = "0.2.10"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
