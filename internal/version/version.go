package version

var Version = "0.12.7"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
