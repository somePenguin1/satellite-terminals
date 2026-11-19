package dev.satfleet.fleetapi.common;

import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

/** Domain exceptions mapped to HTTP status codes; rendered as RFC 7807 problem details. */
public final class ApiExceptions {

    private ApiExceptions() {}

    public static ResponseStatusException notFound(String what) {
        return new ResponseStatusException(HttpStatus.NOT_FOUND, what + " not found");
    }

    /** The request conflicts with the resource's current state (e.g. an invalid lifecycle transition). */
    public static ResponseStatusException conflict(String detail) {
        return new ResponseStatusException(HttpStatus.CONFLICT, detail);
    }

    /** Optimistic concurrency failure: If-Match named a version that is no longer current. */
    public static ResponseStatusException preconditionFailed(String detail) {
        return new ResponseStatusException(HttpStatus.PRECONDITION_FAILED, detail);
    }

    /** Writes to versioned resources must say which version they are based on. */
    public static ResponseStatusException preconditionRequired() {
        return new ResponseStatusException(HttpStatus.PRECONDITION_REQUIRED,
                "Send If-Match with the ETag from your last read so concurrent edits aren't lost");
    }

    public static ResponseStatusException badRequest(String detail) {
        return new ResponseStatusException(HttpStatus.BAD_REQUEST, detail);
    }
}
