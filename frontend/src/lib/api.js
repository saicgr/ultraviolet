// Hand-typed API client. The OpenAPI-generated client lands in Phase 1.5.
const BASE = "/api/v1";
async function request(method, path, body) {
    const res = await fetch(BASE + path, {
        method,
        headers: { "Content-Type": "application/json" },
        body: body ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) {
        const text = await res.text();
        throw new Error(`${res.status}: ${text}`);
    }
    if (res.status === 204)
        return undefined;
    return (await res.json());
}
export const api = {
    listCustomers: () => request("GET", "/customers"),
    createCustomer: (slug, display_name) => request("POST", "/customers", { slug, display_name }),
    listConnections: (customerId) => request("GET", `/customers/${customerId}/connections`),
    createConnection: (customerId, body) => request("POST", `/customers/${customerId}/connections`, body),
    listAPIKeys: (customerId) => request("GET", `/customers/${customerId}/api-keys`),
    createAPIKey: (customerId, description) => request("POST", `/customers/${customerId}/api-keys`, { description }),
    revokeAPIKey: (id) => request("DELETE", `/api-keys/${id}`),
    listSyncedTables: (connectionId) => request("GET", `/connections/${connectionId}/synced-tables`),
    createSyncedTable: (connectionId, body) => request("POST", `/connections/${connectionId}/synced-tables`, body),
    queryAnalytics: (customerId) => request("GET", `/customers/${customerId}/queries`),
    savings: (customerId) => request("GET", `/customers/${customerId}/savings`),
};
