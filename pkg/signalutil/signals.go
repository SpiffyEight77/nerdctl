/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package signalutil

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
)

// killer is from https://github.com/containerd/containerd/blob/v1.7.0-rc.2/cmd/ctr/commands/signals.go#L30-L32
type killer interface {
	Kill(context.Context, syscall.Signal, ...containerd.KillOpts) error
}

// ForwardAllSignals forwards signals.
// From https://github.com/containerd/containerd/blob/v1.7.0-rc.2/cmd/ctr/commands/signals.go#L34-L55
func ForwardAllSignals(ctx context.Context, task killer) chan os.Signal {
	sigc := make(chan os.Signal, 128)
	signal.Notify(sigc)
	go func() {
		for s := range sigc {
			if canIgnoreSignal(s) {
				log.G(ctx).Debugf("Ignoring signal %s", s)
				continue
			}
			log.G(ctx).Debug("forwarding signal ", s)
			if err := task.Kill(ctx, s.(syscall.Signal)); err != nil {
				if errdefs.IsNotFound(err) {
					log.G(ctx).WithError(err).Debugf("Not forwarding signal %s", s)
					return
				}
				log.G(ctx).WithError(err).Errorf("forward signal %s", s)
			}
		}
	}()
	return sigc
}

// StopCatch stops and closes a channel.
// From https://github.com/containerd/containerd/blob/v1.7.0-rc.2/cmd/ctr/commands/signals.go#L57-L61
func StopCatch(sigc chan os.Signal) {
	signal.Stop(sigc)
	close(sigc)
}
