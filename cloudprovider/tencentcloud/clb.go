package tencentcloud

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/types"

	kruisev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cperrors "github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/tencentcloud/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClbNetwork              = "TencentCloud-CLB"
	AliasCLB                = "CLB-Network"
	ClbIdsConfigName        = "ClbIds"
	PortProtocolsConfigName = "PortProtocols"
	MinPortConfigName       = "MinPort"
	MaxPortConfigName       = "MaxPort"
)

type portAllocated map[int32]bool

type ClbPlugin struct {
	maxPort int32
	minPort int32
	mutex   sync.RWMutex
}

type clbConfig struct {
	lbIds       []string
	targetPorts []int
	protocols   []corev1.Protocol
}

type allocateInfo struct {
	LbId  string
	Ports []int32
}

func (s *ClbPlugin) Name() string {
	return ClbNetwork
}

func (s *ClbPlugin) Alias() string {
	return AliasCLB
}

func (s *ClbPlugin) Init(c client.Client, options cloudprovider.CloudProviderOptions, ctx context.Context) error {
	return nil
}

func (s *ClbPlugin) OnPodAdded(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	return pod, nil
}

func (s *ClbPlugin) OnPodUpdated(c client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, cperrors.PluginError) {
	gss, err := util.GetGameServerSetOfPod(pod, c, ctx)
	if err != nil {
		if errors.IsNotFound(err) { // ignore if gss it not found
			return pod, nil
		}
		return pod, cperrors.ToPluginError(err, cperrors.InternalError)
	}
	svc := &v1alpha1.DedicatedCLBService{}
	networkManager := utils.NewNetworkManager(pod, c)
	err = c.Get(ctx, types.NamespacedName{
		Name:      gss.GetName(),
		Namespace: gss.GetNamespace(),
	}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			svc, err = s.consSvc(networkManager.GetNetworkConfig(), gss, ctx)
			if err != nil {
				return pod, cperrors.ToPluginError(err, cperrors.ParameterError)
			}
			return pod, cperrors.ToPluginError(c.Create(ctx, svc), cperrors.ApiCallError)
		}
		return pod, cperrors.NewPluginError(cperrors.ApiCallError, err.Error())
	}
	return pod, nil
}

func (s *ClbPlugin) OnPodDeleted(c client.Client, pod *corev1.Pod, ctx context.Context) cperrors.PluginError {
	return nil
}

func (s *ClbPlugin) consSvc(conf []kruisev1alpha1.NetworkConfParams, gss *kruisev1alpha1.GameServerSet, _ context.Context) (*v1alpha1.DedicatedCLBService, error) {
	svc := &v1alpha1.DedicatedCLBService{}
	svc.Namespace = gss.GetNamespace()
	svc.Name = gss.GetName()
	svc.Spec.Selector = map[string]string{
		kruisev1alpha1.GameServerOwnerGssKey: gss.GetName(),
	}
	svc.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         gss.APIVersion,
			Kind:               gss.Kind,
			Name:               gss.GetName(),
			UID:                gss.GetUID(),
			Controller:         ptr.To[bool](true),
			BlockOwnerDeletion: ptr.To[bool](true),
		},
	}

	for _, c := range conf {
		switch c.Name {
		case ClbIdsConfigName:
			for _, lbId := range strings.Split(c.Value, ",") {
				if lbId != "" {
					svc.Spec.ExistedLbIds = append(svc.Spec.ExistedLbIds, lbId)
				}
			}
		case PortProtocolsConfigName:
			for _, pp := range strings.Split(c.Value, ",") {
				ppSlice := strings.Split(pp, "/")
				targetPort, err := strconv.ParseInt(ppSlice[0], 10, 64)
				if err != nil {
					continue
				}
				port := v1alpha1.DedicatedCLBServicePort{}
				port.TargetPort = targetPort
				if len(ppSlice) != 2 {
					port.Protocol = string(corev1.ProtocolTCP)
				} else {
					port.Protocol = ppSlice[1]
				}
				svc.Spec.Ports = append(svc.Spec.Ports, port)
			}
		case MinPortConfigName:
			minPort, err := strconv.ParseInt(c.Value, 10, 64)
			if err != nil {
				continue
			}
			svc.Spec.MinPort = minPort
		case MaxPortConfigName:
			maxPort, err := strconv.ParseInt(c.Value, 10, 64)
			if err != nil {
				continue
			}
			svc.Spec.MaxPort = maxPort
		}
	}
	return svc, nil
}

func init() {
	clbPlugin := ClbPlugin{
		mutex: sync.RWMutex{},
	}
	tencentCloudProvider.registerPlugin(&clbPlugin)
}
