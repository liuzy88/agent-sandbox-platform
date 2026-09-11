# Agent Sandbox Platform

> 本项目基于k8s官方的 [Agent Sandbox](https://agent-sandbox.sigs.k8s.io/docs/getting_started/overview/)

> 本项目实现对沙箱运行时的全生命周期控制。定义交互接口：创建、查询、事件流、人工输入、实时引导、取消销毁等。

## 一、整体架构



## 二、依赖组件

### 1、集群

首先需要一个k8s集群，并安装沙箱控制器扩展，向外提供集群资源控制权
```shell
VERSION=1.0.2
kubectl apply -f https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${VERSION}/sandbox-with-extensions.yaml
```

### 2、中间件

## 三、使用手册

### 1、快速开始（本地MacOS环境）

### 2、开发调试（内网K8S环境）

### 3、生产需知（云环境）
