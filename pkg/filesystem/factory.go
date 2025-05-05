package filesystem

type fileFactory struct {
}

var (
	_ FileFactory = &fileFactory{}
)

var Factory = &fileFactory{}
