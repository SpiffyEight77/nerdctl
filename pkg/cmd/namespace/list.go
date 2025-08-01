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

package namespace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"text/template"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/log"

	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/clientutil"
	"github.com/containerd/nerdctl/v2/pkg/formatter"
	"github.com/containerd/nerdctl/v2/pkg/mountutil/volumestore"
)

func List(ctx context.Context, client *containerd.Client, options types.NamespaceListOptions) error {
	nsStore := client.NamespaceService()
	nsList, err := nsStore.List(ctx)
	if err != nil {
		return err
	}

	dataStore, err := clientutil.DataStore(options.GOptions.DataRoot, options.GOptions.Address)
	if err != nil {
		return err
	}

	w := options.Stdout
	var tmpl *template.Template
	namespaceMap := make(map[string]namespace, len(nsList))
	for _, ns := range nsList {
		ctx = namespaces.WithNamespace(ctx, ns)
		var numContainers, numImages, numVolumes int
		var labelStrings []string

		containers, err := client.Containers(ctx)
		if err != nil {
			log.L.Warn(err)
		}
		numContainers = len(containers)

		images, err := client.ImageService().List(ctx)
		if err != nil {
			log.L.Warn(err)
		}
		numImages = len(images)

		volStore, err := volumestore.New(dataStore, ns)
		if err != nil {
			log.L.Warn(err)
		} else {
			numVolumes, err = volStore.Count()
			if err != nil {
				log.L.Warn(err)
			}
		}

		labels, err := client.NamespaceService().Labels(ctx, ns)
		if err != nil {
			return err
		}
		for k, v := range labels {
			labelStrings = append(labelStrings, strings.Join([]string{k, v}, "="))
		}
		sort.Strings(labelStrings)
		namespaceMap[ns] = namespace{
			name:       ns,
			containers: numContainers,
			images:     numImages,
			volumes:    numVolumes,
			labels:     labelStrings,
		}
	}

	switch options.Format {
	case "", "table", "wide":
		if !options.Quiet {
			w = tabwriter.NewWriter(w, 4, 8, 4, ' ', 0)
			// no "NETWORKS", because networks are global objects
			fmt.Fprintln(w, "NAME\tCONTAINERS\tIMAGES\tVOLUMES\tLABELS")
		}
	case "raw":
		return errors.New("unsupported format: \"raw\"")
	default:
		if options.Quiet {
			return errors.New("format and quiet must not be specified together")
		}
		var err error
		tmpl, err = formatter.ParseTemplate(options.Format)
		if err != nil {
			return err
		}
	}

	for _, namespace := range namespaceMap {
		if tmpl != nil {
			labels, err := nsStore.Labels(ctx, namespace.name)
			if err != nil {
				return err
			}

			ns := map[string]interface{}{
				"Name":       namespace.name,
				"Containers": namespace.containers,
				"Images":     namespace.images,
				"Volumes":    namespace.volumes,
				"Labels":     labels,
			}

			var b bytes.Buffer
			if err := tmpl.Execute(&b, ns); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w, b.String()); err != nil {
				return err
			}
		} else if options.Quiet {
			if _, err := fmt.Fprintln(w, namespace.name); err != nil {
				return err
			}
		} else {
			format := "%s\t%d\t%d\t%d\t%v\t\n"
			args := []interface{}{}
			args = append(args, namespace.name, namespace.containers, namespace.images, namespace.volumes, strings.Join(namespace.labels, ","))
			if _, err := fmt.Fprintf(w, format, args...); err != nil {
				return err
			}
		}
	}

	if f, ok := w.(formatter.Flusher); ok {
		return f.Flush()
	}
	return nil
}

type namespace struct {
	name       string
	containers int
	images     int
	volumes    int
	labels     []string
}
