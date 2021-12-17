package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	addonsv1alpha1 "github.com/openshift/addon-operator/apis/addons/v1alpha1"
)

// addonState is a helper type that will help us
// track the conditions for an addon, in-memory.
// This state will be used for updating condition metrics.
type addonState struct {
	conditionMap map[string]addonConditions
	lock         sync.RWMutex
}

type addonConditions struct {
	available bool
	paused    bool
}

// Recorder stores all Addon related metrics
type Recorder struct {
	addonState *addonState

	// metrics
	addonsTotalAvailable  *prometheus.GaugeVec
	addonsTotalPaused     *prometheus.GaugeVec
	addonsTotal           *prometheus.GaugeVec
	addonOperatorPaused   *prometheus.GaugeVec // 0 - Not paused , 1 - Paused
	ocmAPIRequestDuration prometheus.Summary
	// .. TODO: More metrics!
}

func NewRecorder() *Recorder {
	addonsAvailable := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "addon_operator_addons_available",
			Help: "Total number of Addons available",
		}, []string{})

	addonsPaused := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "addon_operator_addons_paused",
			Help: "Total number of Addons paused",
		}, []string{})

	addonsTotal := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "addon_operator_addons_total",
			Help: "Total number of Addon installations",
		}, []string{})

	addonOperatorPaused := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "addon_operator_paused",
			Help: "A boolean that tells if the AddonOperator is paused",
		}, []string{})

	ocmAPIReqDuration := prometheus.NewSummary(
		prometheus.SummaryOpts{
			Name: "addon_operator_ocm_api_requests_durations",
			Help: "OCM API request latencies in microseconds",
			// p50, p90 and p99 latencies
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		})

	// Register metrics
	ctrlmetrics.Registry.MustRegister(
		addonsTotal,
		addonsAvailable,
		addonsPaused,
		addonOperatorPaused,
		ocmAPIReqDuration,
	)

	return &Recorder{
		addonState: &addonState{
			conditionMap: map[string]addonConditions{},
		},
		addonsTotal:           addonsTotal,
		addonsTotalAvailable:  addonsAvailable,
		addonsTotalPaused:     addonsPaused,
		addonOperatorPaused:   addonOperatorPaused,
		ocmAPIRequestDuration: ocmAPIReqDuration,
	}
}

func (r *Recorder) increaseAvailableAddonsCount() {
	r.addonsTotalAvailable.WithLabelValues().Inc()
}

func (r *Recorder) decreaseAvailableAddonsCount() {
	r.addonsTotalAvailable.WithLabelValues().Dec()
}

func (r *Recorder) increasePausedAddonsCount() {
	r.addonsTotalPaused.WithLabelValues().Inc()
}

func (r *Recorder) decreasePausedAddonsCount() {
	r.addonsTotalPaused.WithLabelValues().Dec()
}

func (r *Recorder) increaseTotalAddonsCount() {
	r.addonsTotal.WithLabelValues().Inc()
}

func (r *Recorder) decreaseTotalAddonsCount() {
	r.addonsTotal.WithLabelValues().Dec()
}

func (r *Recorder) ObserveOCMAPIRequests(us float64) {
	r.ocmAPIRequestDuration.Observe(us)
}

// SetAddonOperatorPaused sets the `addon_operator_paused` metric
// 0 - Not paused , 1 - Paused
func (r *Recorder) SetAddonOperatorPaused(paused bool) {
	if paused {
		r.addonOperatorPaused.WithLabelValues().Set(1)
	} else {
		r.addonOperatorPaused.WithLabelValues().Set(0)

	}
}

// HandleAddonConditionAndInstallCount is responsible for reconciling the following metrics:
// - addon_operator_addons_available
// - addon_operator_addons_paused
// - addon_operator_addons_total
func (r *Recorder) HandleAddonConditionAndInstallCount(addonUID string,
	conditions []metav1.Condition,
	uninstall bool) {
	r.addonState.lock.Lock()
	defer r.addonState.lock.Unlock()

	currCondition := addonConditions{
		available: meta.IsStatusConditionTrue(conditions, addonsv1alpha1.Available),
		paused:    meta.IsStatusConditionTrue(conditions, addonsv1alpha1.Paused),
	}

	oldCondition, ok := r.addonState.conditionMap[addonUID]

	// handle new Addon installations
	if !ok {
		r.addonState.conditionMap[addonUID] = currCondition
		r.increaseTotalAddonsCount()
		if currCondition.available {
			r.increaseAvailableAddonsCount()
		}

		if currCondition.paused {
			r.increasePausedAddonsCount()
		}
		return
	}

	// reconcile metrics for existing Addons
	if oldCondition != currCondition {
		if oldCondition.available != currCondition.available {
			if currCondition.available {
				r.increaseAvailableAddonsCount()
			} else {
				r.decreaseAvailableAddonsCount()
			}
		}

		if oldCondition.paused != currCondition.paused {
			if currCondition.paused {
				r.increasePausedAddonsCount()
			} else {
				r.decreasePausedAddonsCount()
			}
		}

		// Update the current Addon conditions in the in-memory map
		r.addonState.conditionMap[addonUID] = currCondition
	}

	// handle new Addon uninstallations
	if uninstall {
		r.decreaseTotalAddonsCount()
		if currCondition.available {
			r.decreaseAvailableAddonsCount()
		}

		if currCondition.paused {
			r.decreasePausedAddonsCount()
		}
		delete(r.addonState.conditionMap, addonUID)
	}
}
