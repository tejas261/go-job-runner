terraform {
  required_version = ">=1.3"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~>3.100"
    }
  }

  backend "azurerm" {
    resource_group_name  = "tfstate-rg"
    storage_account_name = "tfstatejobrunner"
    container_name       = "tfstate"
    key                  = "terraform.tfstate"
  }
}

provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "job-runner" {
  name     = "job-runner-resources"
  location = "South India"
}

resource "azurerm_virtual_network" "job-runner" {
  name                = "job-runner_VNet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.job-runner.location
  resource_group_name = azurerm_resource_group.job-runner.name
}

resource "azurerm_subnet" "aks" {
  name                 = "aks-subnet"
  resource_group_name  = azurerm_resource_group.job-runner.name
  virtual_network_name = azurerm_virtual_network.job-runner.name
  address_prefixes     = ["10.0.1.0/24"]
}

resource "azurerm_kubernetes_cluster" "k8s" {
  name                = "job-runner-k8s"
  location            = azurerm_resource_group.job-runner.location
  resource_group_name = azurerm_resource_group.job-runner.name
  dns_prefix          = "job-runner-aks"

  default_node_pool {
    name       = "agentpool"
    node_count = 2
    vm_size = "Standard_D2s_v3"
  }
  identity {
    type = "SystemAssigned"
  }
}
