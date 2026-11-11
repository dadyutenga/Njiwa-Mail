/** Network calls are opt-in: configure VITE_API_BASE_URL for your own service. */
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}
type Interceptor = (request: RequestInit) => RequestInit;
const interceptors: Interceptor[] = [];
type ResponseInterceptor = (response: Response) => Response | Promise<Response>;
const responseInterceptors: ResponseInterceptor[] = [];
export function addResponseInterceptor(interceptor: ResponseInterceptor) {
  responseInterceptors.push(interceptor);
  return () => {
    const index = responseInterceptors.indexOf(interceptor);
    if (index >= 0) responseInterceptors.splice(index, 1);
  };
}
export function addRequestInterceptor(interceptor: Interceptor) {
  interceptors.push(interceptor);
  return () => {
    const index = interceptors.indexOf(interceptor);
    if (index >= 0) interceptors.splice(index, 1);
  };
}
export async function apiRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const base = import.meta.env.VITE_API_BASE_URL;
  if (!base) throw new ApiError("Configure VITE_API_BASE_URL before making requests", 0);
  const request = interceptors.reduce((current, interceptor) => interceptor(current), options);
  let response: Response;
  try {
    response = await fetch(new URL(path, base), request);
    for (const interceptor of responseInterceptors) response = await interceptor(response);
  } catch {
    throw new ApiError("Network request failed", 0);
  }
  if (!response.ok) throw new ApiError(response.statusText || "Request failed", response.status);
  if (response.status === 204) return undefined as T;
  try {
    return (await response.json()) as T;
  } catch {
    throw new ApiError("The server returned invalid JSON", response.status);
  }
}
