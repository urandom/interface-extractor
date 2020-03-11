package bar

type Streamer struct{}

func (s Streamer) Stream() <-chan Alpha {
	return nil
}

type StreamConsumer struct{}

func (c StreamConsumer) Consume(_ <-chan Alpha) {
}
