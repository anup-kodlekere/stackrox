package processlisteningonport

import (
	"context"

	countMetrics "github.com/stackrox/rox/central/metrics"
	"github.com/stackrox/rox/central/processlisteningonport/datastore"
	"github.com/stackrox/rox/central/sensor/service/common"
	"github.com/stackrox/rox/central/sensor/service/pipeline"
	"github.com/stackrox/rox/central/sensor/service/pipeline/reconciliation"
	"github.com/stackrox/rox/generated/internalapi/central"
	"github.com/stackrox/rox/pkg/logging"
	"github.com/stackrox/rox/pkg/metrics"
)

var (
	log = logging.LoggerForModule()
)

// GetPipeline returns an instantiation of this particular pipeline
func GetPipeline() pipeline.Fragment {
	return &pipelineImpl{
		dataStore: datastore.Singleton(),
	}
}

type pipelineImpl struct {
	dataStore datastore.DataStore
}

func (s *pipelineImpl) Reconcile(
	ctx context.Context,
	clusterID string,
	_ *reconciliation.StoreMap,
) error {

	// Nothing to reconcile
	return nil
}

func (s *pipelineImpl) Match(msg *central.MsgFromSensor) bool {
	return msg.GetProcessListeningOnPortUpdate() != nil
}

// Run runs the pipeline template on the input and returns the output.
func (s *pipelineImpl) Run(
	ctx context.Context,
	clusterID string,
	msg *central.MsgFromSensor,
	injector common.MessageInjector,
) error {
	defer countMetrics.IncrementResourceProcessedCounter(
		pipeline.ActionToOperation(msg.GetEvent().GetAction()), metrics.ProcessListeningOnPort)

	update := msg.GetProcessListeningOnPortUpdate()
	if s.dataStore != nil && update != nil {
		portProcesses := update.GetProcessesListeningOnPorts()

		if portProcesses != nil {
			log.Debugf("Store PLOP object: %+v", portProcesses)
			if err := s.dataStore.AddProcessListeningOnPort(ctx, portProcesses...); err != nil {
				return err
			}
		}
	} else {
		if s.dataStore == nil {
			log.Warnf("Cannot process PLOP event: data store is nil")
		}

		if update == nil {
			log.Warnf("Cannot process PLOP event: update message is nil")
		}
	}

	return nil
}

func (s *pipelineImpl) OnFinish(clusterID string) {}
