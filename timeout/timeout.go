package timeout

import "time"

type Pausable interface {
	Pause()
}

type Resumable interface {
	Resume()
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

func (ap *AutoPauser) Start() {
	ticker := time.NewTicker(ap.tick)
	ap.last = time.Now()

	for {
		select {
		case now := <-ticker.C:
			if now.After(ap.last.Add(ap.timeout)) {
				ticker.Stop()
				ap.instance.Pause()

				// Wait for resume.
				<-ap.resetChan

				// Set last reset time, resume instance and reset the ticker.
				ap.last = time.Now()
				ticker = time.NewTicker(ap.tick)
				ap.instance.Resume()
			}
		case <-ap.resetChan:
			ap.last = time.Now()
		}
	}
}

func (ap *AutoPauser) Reset() {
	ap.resetChan <- struct{}{}
}
