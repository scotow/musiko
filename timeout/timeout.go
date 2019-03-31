package timeout

import "time"

type Pausable interface {
	Pause() error
}

type Resumable interface {
	Resume() error
}

type PauseResumable interface {
	Pausable
	Resumable
}

type AutoPauser struct {
	instance PauseResumable
	timeout  time.Duration
	tick     time.Duration
	last     time.Time

	resetChan chan struct{}
}

func NewAutoPauser(instance PauseResumable, timeout, tick time.Duration) *AutoPauser {
	nap := new(AutoPauser)
	nap.instance = instance
	nap.timeout = timeout
	nap.tick = tick
	nap.resetChan = make(chan struct{})

	return nap
}

func (ap *AutoPauser) Start() error {
	ticker := time.NewTicker(ap.tick)
	ap.last = time.Now()

	for {
		select {
		case now := <-ticker.C:
			if now.After(ap.last.Add(ap.timeout)) {
				// Stop the ticker and pause the stream.
				ticker.Stop()
				err := ap.instance.Pause()
				if err != nil {
					return err
				}

				// Wait for resume.
				<-ap.resetChan

				// Set last reset time, resume instance and restart the ticker.
				ap.last = time.Now()
				err = ap.instance.Resume()
				if err != nil {
					return err
				}
				ticker = time.NewTicker(ap.tick)
			}
		case <-ap.resetChan:
			ap.last = time.Now()
		}
	}
}

func (ap *AutoPauser) Reset() {
	ap.resetChan <- struct{}{}
}
