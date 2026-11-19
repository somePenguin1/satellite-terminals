package dev.satfleet.fleetapi.terminals;

import dev.satfleet.fleetapi.common.Cursor;
import dev.satfleet.fleetapi.terminals.TerminalModels.CursorPage;
import dev.satfleet.fleetapi.terminals.TerminalModels.Filter;
import dev.satfleet.fleetapi.terminals.TerminalModels.ProvisionRequest;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalDetail;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalPatch;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalSummary;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalV1Item;
import io.swagger.v3.oas.annotations.Operation;
import jakarta.validation.Valid;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import java.net.URI;
import java.util.List;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PatchMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class TerminalController {

    private final TerminalService service;
    private final TerminalRepository repo;

    public TerminalController(TerminalService service, TerminalRepository repo) {
        this.service = service;
        this.repo = repo;
    }

    private static String blankToNull(String s) {
        return s == null || s.isBlank() ? null : s.trim();
    }

    private static Filter filter(String q, String status, String health, String region, Long customerId) {
        return new Filter(blankToNull(q), blankToNull(status), blankToNull(health), blankToNull(region), customerId);
    }

    @Operation(summary = "List terminals (v1: flat items, offset pagination, total in X-Total-Count)")
    @GetMapping("/api/v1/terminals")
    public ResponseEntity<List<TerminalV1Item>> listV1(
            @RequestParam(required = false) String q,
            @RequestParam(required = false) String status,
            @RequestParam(required = false) String health,
            @RequestParam(required = false) String region,
            @RequestParam(required = false) Long customerId,
            @RequestParam(defaultValue = "0") @Min(0) int offset,
            @RequestParam(defaultValue = "50") @Min(1) @Max(500) int limit) {
        Filter f = filter(q, status, health, region, customerId);
        return ResponseEntity.ok()
                .header("X-Total-Count", String.valueOf(repo.count(f)))
                .body(repo.findOffset(f, offset, limit));
    }

    @Operation(summary = "List terminals (v2: nested items with live metrics, keyset cursor pagination)")
    @GetMapping("/api/v2/terminals")
    public CursorPage<TerminalSummary> listV2(
            @RequestParam(required = false) String q,
            @RequestParam(required = false) String status,
            @RequestParam(required = false) String health,
            @RequestParam(required = false) String region,
            @RequestParam(required = false) Long customerId,
            @RequestParam(required = false) String cursor,
            @RequestParam(defaultValue = "50") @Min(1) @Max(500) int limit) {
        Filter f = filter(q, status, health, region, customerId);
        // Fetch one extra row to learn whether another page exists without a second query.
        List<TerminalSummary> rows = repo.findPage(f, Cursor.decode(cursor), limit + 1);
        boolean more = rows.size() > limit;
        List<TerminalSummary> items = more ? rows.subList(0, limit) : rows;
        String next = more ? Cursor.encode(items.get(items.size() - 1).id()) : null;
        return new CursorPage<>(items, next, repo.count(f));
    }

    @GetMapping("/api/v1/terminals/{id}")
    public TerminalDetail get(@PathVariable String id) {
        return service.get(id);
    }

    @Operation(summary = "Order a new terminal for a customer; it starts in the provisioned state")
    @PostMapping("/api/v1/terminals")
    public ResponseEntity<TerminalDetail> provision(
            @Valid @RequestBody ProvisionRequest req,
            @RequestHeader(name = "X-Actor", defaultValue = "api") String actor) {
        TerminalDetail created = service.provision(req, actor);
        return ResponseEntity.created(URI.create("/api/v1/terminals/" + created.id())).body(created);
    }

    @Operation(summary = "Change service status (activate, suspend, resume, decommission) and/or plan")
    @PatchMapping("/api/v1/terminals/{id}")
    public TerminalDetail patch(@PathVariable String id, @Valid @RequestBody TerminalPatch patch) {
        return service.patch(id, patch);
    }
}
