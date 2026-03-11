import { setPath } from './configUtils';

type UseConfigGatewayActionsArgs = {
  setCfg: React.Dispatch<React.SetStateAction<any>>;
};

export function useConfigGatewayActions({ setCfg }: UseConfigGatewayActionsArgs) {
  function updateGatewayP2PField(field: string, value: any) {
    setCfg((current) => setPath(current, `gateway.nodes.p2p.${field}`, value));
  }

  function updateGatewayDispatchField(field: string, value: any) {
    setCfg((current) => setPath(current, `gateway.nodes.dispatch.${field}`, value));
  }

  function updateGatewayArtifactsField(field: string, value: any) {
    setCfg((current) => setPath(current, `gateway.nodes.artifacts.${field}`, value));
  }

  function updateGatewayIceServer(index: number, field: string, value: any) {
    setCfg((current) => {
      const next = JSON.parse(JSON.stringify(current || {}));
      if (!next.gateway || typeof next.gateway !== 'object') next.gateway = {};
      if (!next.gateway.nodes || typeof next.gateway.nodes !== 'object') next.gateway.nodes = {};
      if (!next.gateway.nodes.p2p || typeof next.gateway.nodes.p2p !== 'object') next.gateway.nodes.p2p = {};
      if (!Array.isArray(next.gateway.nodes.p2p.ice_servers)) next.gateway.nodes.p2p.ice_servers = [];
      if (!next.gateway.nodes.p2p.ice_servers[index] || typeof next.gateway.nodes.p2p.ice_servers[index] !== 'object') {
        next.gateway.nodes.p2p.ice_servers[index] = { urls: [], username: '', credential: '' };
      }
      next.gateway.nodes.p2p.ice_servers[index][field] = value;
      return next;
    });
  }

  function addGatewayIceServer() {
    setCfg((current) => {
      const next = JSON.parse(JSON.stringify(current || {}));
      if (!next.gateway || typeof next.gateway !== 'object') next.gateway = {};
      if (!next.gateway.nodes || typeof next.gateway.nodes !== 'object') next.gateway.nodes = {};
      if (!next.gateway.nodes.p2p || typeof next.gateway.nodes.p2p !== 'object') next.gateway.nodes.p2p = {};
      if (!Array.isArray(next.gateway.nodes.p2p.ice_servers)) next.gateway.nodes.p2p.ice_servers = [];
      next.gateway.nodes.p2p.ice_servers.push({ urls: [], username: '', credential: '' });
      return next;
    });
  }

  function removeGatewayIceServer(index: number) {
    setCfg((current) => {
      const next = JSON.parse(JSON.stringify(current || {}));
      const iceServers = next?.gateway?.nodes?.p2p?.ice_servers;
      if (Array.isArray(iceServers)) {
        iceServers.splice(index, 1);
      }
      return next;
    });
  }

  return {
    addGatewayIceServer,
    removeGatewayIceServer,
    updateGatewayArtifactsField,
    updateGatewayDispatchField,
    updateGatewayIceServer,
    updateGatewayP2PField,
  };
}
