import { Counter, Rate, Trend } from 'k6/metrics';

export const routeDuration = new Trend('route_duration', true);
export const routeSuccess = new Rate('route_success');
export const schemaSuccess = new Rate('schema_success');
export const secretLeaks = new Counter('secret_leaks');
export const authSuccess = new Rate('auth_success');
export const negotiationSuccess = new Rate('negotiation_success');
export const openapiSuccess = new Rate('openapi_success');
