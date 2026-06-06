package version

var Version = "0.18.7"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
