package main

import (
	"fmt"
	"github.com/pulumi/pulumi-azure-native-sdk/compute/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/network/v2"
	"github.com/pulumi/pulumi-azure/sdk/v6/go/azure/containerservice"
	"github.com/pulumi/pulumi-azure/sdk/v6/go/azure/core"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	tls "github.com/pulumi/pulumi-tls/sdk/v4/go/tls"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"strings"
)

func vm(ctx *pulumi.Context, component string, rg *core.ResourceGroup) error {
	vmConfig := config.New(ctx, component)
	adminUsername := vmConfig.Require("adminUsername")
	osImage := vmConfig.Require("osImage")
	vmName := vmConfig.Require("vmName")
	vmSize := vmConfig.Require("vmSize")
	vmAddressPrefix := vmConfig.Require("vmAddressPrefix")

	osImageArgs := strings.Split(osImage, ":")
	osImagePublisher := osImageArgs[0]
	osImageOffer := osImageArgs[1]
	osImageSku := osImageArgs[2]
	osImageVersion := osImageArgs[3]

	// Create an SSH key
	sshKey, err := tls.NewPrivateKey(ctx, fmt.Sprintf("%v-ssh-key", vmName), &tls.PrivateKeyArgs{
		Algorithm: pulumi.String("RSA"),
		RsaBits:   pulumi.Int(4096),
	})

	if err != nil {
		return err
	}

	// Create a virtual network
	virtualNetwork, err := network.NewVirtualNetwork(ctx, fmt.Sprintf("%v-network", vmName), &network.VirtualNetworkArgs{
		ResourceGroupName: rg.Name,
		AddressSpace: network.AddressSpaceArgs{
			AddressPrefixes: pulumi.ToStringArray([]string{vmAddressPrefix}),
		},
		Subnets: network.SubnetTypeArray{
			network.SubnetTypeArgs{
				Name:          pulumi.Sprintf("%s-subnet", vmName),
				AddressPrefix: pulumi.String(vmAddressPrefix),
			},
		},
	})
	if err != nil {
		return err
	}

	// Use a random string to give the VM a unique DNS name
	domainLabelSuffix, err := random.NewRandomString(ctx, fmt.Sprintf("%v-domain-label", vmName), &random.RandomStringArgs{
		Length:  pulumi.Int(8),
		Upper:   pulumi.Bool(false),
		Special: pulumi.Bool(false),
	})
	if err != nil {
		return err
	}

	domainLabel := domainLabelSuffix.Result.ApplyT(func(result string) string {
		return fmt.Sprintf("%s-%s", vmName, result)
	}).(pulumi.StringOutput)

	// Create a public IP address for the VM
	publicIp, err := network.NewPublicIPAddress(ctx, fmt.Sprintf("%v-public-ip", vmName), &network.PublicIPAddressArgs{
		ResourceGroupName:        rg.Name,
		PublicIPAllocationMethod: pulumi.StringPtr("Dynamic"),
		DnsSettings: network.PublicIPAddressDnsSettingsArgs{
			DomainNameLabel: domainLabel,
		},
	})
	if err != nil {
		return err
	}

	// Create a security group allowing inbound access over port 22 (for SSH)
	securityGroup, err := network.NewNetworkSecurityGroup(ctx, fmt.Sprintf("%v-security-group", vmName), &network.NetworkSecurityGroupArgs{
		ResourceGroupName: rg.Name,
		SecurityRules: network.SecurityRuleTypeArray{
			network.SecurityRuleTypeArgs{
				Name:                     pulumi.StringPtr(fmt.Sprintf("%s-securityrule", vmName)),
				Priority:                 pulumi.Int(1000),
				Direction:                pulumi.String("Inbound"),
				Access:                   pulumi.String("Allow"),
				Protocol:                 pulumi.String("Tcp"),
				SourcePortRange:          pulumi.StringPtr("*"),
				SourceAddressPrefix:      pulumi.StringPtr("*"),
				DestinationAddressPrefix: pulumi.StringPtr("*"),
				DestinationPortRanges: pulumi.ToStringArray([]string{
					"22",
				}),
			},
		},
	})
	if err != nil {
		return err
	}

	// Create a network interface with the virtual network, IP address, and security group
	networkInterface, err := network.NewNetworkInterface(ctx, fmt.Sprintf("%v-security-group", vmName), &network.NetworkInterfaceArgs{
		ResourceGroupName: rg.Name,
		NetworkSecurityGroup: &network.NetworkSecurityGroupTypeArgs{
			Id: securityGroup.ID(),
		},
		IpConfigurations: network.NetworkInterfaceIPConfigurationArray{
			&network.NetworkInterfaceIPConfigurationArgs{
				Name:                      pulumi.String(fmt.Sprintf("%s-ipconfiguration", vmName)),
				PrivateIPAllocationMethod: pulumi.String("Dynamic"),
				Subnet: &network.SubnetTypeArgs{
					Id: virtualNetwork.Subnets.ApplyT(func(subnets []network.SubnetResponse) (string, error) {
						return *subnets[0].Id, nil
					}).(pulumi.StringOutput),
				},
				PublicIPAddress: &network.PublicIPAddressTypeArgs{
					Id: publicIp.ID(),
				},
			},
		},
	})
	if err != nil {
		return err
	}

	// Create the virtual machine
	vm, err := compute.NewVirtualMachine(ctx, vmName, &compute.VirtualMachineArgs{
		Location:          rg.Location,
		ResourceGroupName: rg.Name,
		NetworkProfile: &compute.NetworkProfileArgs{
			NetworkInterfaces: compute.NetworkInterfaceReferenceArray{
				&compute.NetworkInterfaceReferenceArgs{
					Id:      networkInterface.ID(),
					Primary: pulumi.Bool(true),
				},
			},
		},
		HardwareProfile: &compute.HardwareProfileArgs{
			VmSize: pulumi.String(vmSize),
		},
		OsProfile: &compute.OSProfileArgs{
			ComputerName:  pulumi.String(vmName),
			AdminUsername: pulumi.String(adminUsername),
			LinuxConfiguration: &compute.LinuxConfigurationArgs{
				DisablePasswordAuthentication: pulumi.Bool(true),
				Ssh: &compute.SshConfigurationArgs{
					PublicKeys: compute.SshPublicKeyTypeArray{
						&compute.SshPublicKeyTypeArgs{
							KeyData: sshKey.PublicKeyOpenssh,
							Path:    pulumi.String(fmt.Sprintf("/home/%v/.ssh/authorized_keys", adminUsername)),
						},
					},
				},
			},
		},
		StorageProfile: &compute.StorageProfileArgs{
			OsDisk: &compute.OSDiskArgs{
				Name:         pulumi.String(fmt.Sprintf("%v-osdisk", vmName)),
				DeleteOption: pulumi.String("Delete"),
				CreateOption: pulumi.String(compute.DiskCreateOptionTypesFromImage),
			},
			ImageReference: &compute.ImageReferenceArgs{
				Publisher: pulumi.String(osImagePublisher),
				Offer:     pulumi.String(osImageOffer),
				Sku:       pulumi.String(osImageSku),
				Version:   pulumi.String(osImageVersion),
			},
		},
		Tags: pulumi.StringMap{
			"Environment": pulumi.String("test"),
		},
	})
	if err != nil {
		return err
	}

	// Once the machine is created, fetch its IP address and DNS hostname
	address := vm.ID().ApplyT(func(_ pulumi.ID) network.LookupPublicIPAddressResultOutput {
		return network.LookupPublicIPAddressOutput(ctx, network.LookupPublicIPAddressOutputArgs{
			ResourceGroupName:   rg.Name,
			PublicIpAddressName: publicIp.Name,
		})
	})

	// Export the VM's hostname, public IP address, HTTP URL, and SSH private key
	ctx.Export(fmt.Sprintf("%s-ip", component), address.ApplyT(func(addr network.LookupPublicIPAddressResult) (string, error) {
		return *addr.IpAddress, nil
	}).(pulumi.StringOutput))
	ctx.Export(fmt.Sprintf("%s-privatekey", component), sshKey.PrivateKeyOpenssh)

	return nil
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		mainConfig := config.New(ctx, "")
		location := mainConfig.Require("location")
		testRG, err := core.NewResourceGroup(ctx, "test", &core.ResourceGroupArgs{
			Name:     pulumi.String("rg-ingress-nginx-public-001"),
			Location: pulumi.String(location),
		})
		if err != nil {
			return err
		}

		aksConfig := config.New(ctx, "aks")
		aksVmSize := aksConfig.Require("vmSize")
		aksNodeCount, _ := aksConfig.TryInt("nodeCount")

		kubernetesCluster, err := containerservice.NewKubernetesCluster(ctx, "testAKS", &containerservice.KubernetesClusterArgs{
			Name:              pulumi.String("aks-ingress-nginx-public"),
			Location:          testRG.Location,
			ResourceGroupName: testRG.Name,
			DnsPrefix:         pulumi.String("aks-ingress-nginx-public"),
			DefaultNodePool: &containerservice.KubernetesClusterDefaultNodePoolArgs{
				Name:      pulumi.String("default"),
				NodeCount: pulumi.Int(aksNodeCount),
				VmSize:    pulumi.String(aksVmSize),
				UpgradeSettings: &containerservice.KubernetesClusterDefaultNodePoolUpgradeSettingsArgs{
					MaxSurge:                  pulumi.String("10%"),
					DrainTimeoutInMinutes:     pulumi.Int(0),
					NodeSoakDurationInMinutes: pulumi.Int(0),
				},
			},
			Identity: &containerservice.KubernetesClusterIdentityArgs{
				Type: pulumi.String("SystemAssigned"),
			},
			Tags: pulumi.StringMap{
				"Environment": pulumi.String("test"),
			},
		})
		if err != nil {
			return err
		}

		vm(ctx, "vm1", testRG)
		vm(ctx, "vm2", testRG)

		ctx.Export("kubeConfig", kubernetesCluster.KubeConfigRaw)

		return nil
	})
}
