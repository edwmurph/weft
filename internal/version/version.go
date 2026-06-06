package version

var Version = "0.18.5"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
