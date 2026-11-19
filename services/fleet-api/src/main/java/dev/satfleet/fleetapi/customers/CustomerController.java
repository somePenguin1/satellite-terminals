package dev.satfleet.fleetapi.customers;

import static dev.satfleet.fleetapi.common.ApiExceptions.notFound;

import dev.satfleet.fleetapi.customers.CustomerRepository.CustomerSummary;
import dev.satfleet.fleetapi.terminals.TerminalModels.Filter;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalSummary;
import dev.satfleet.fleetapi.terminals.TerminalRepository;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import java.time.Instant;
import java.util.List;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/v1/customers")
public class CustomerController {

    public record CustomerDetail(
            long id,
            String name,
            String segment,
            String region,
            String accountManager,
            long terminals,
            long activeTerminals,
            long mrrCents,
            String contactEmail,
            Instant createdAt,
            List<TerminalSummary> terminalList) {}

    private final CustomerRepository customers;
    private final TerminalRepository terminals;

    public CustomerController(CustomerRepository customers, TerminalRepository terminals) {
        this.customers = customers;
        this.terminals = terminals;
    }

    @GetMapping
    public ResponseEntity<List<CustomerSummary>> list(
            @RequestParam(required = false) String q,
            @RequestParam(required = false) String segment,
            @RequestParam(defaultValue = "0") @Min(0) int offset,
            @RequestParam(defaultValue = "50") @Min(1) @Max(500) int limit) {
        String query = q == null || q.isBlank() ? null : q.trim();
        String seg = segment == null || segment.isBlank() ? null : segment;
        return ResponseEntity.ok()
                .header("X-Total-Count", String.valueOf(customers.count(query, seg)))
                .body(customers.list(query, seg, offset, limit));
    }

    @GetMapping("/{id}")
    public CustomerDetail get(@PathVariable long id) {
        var rec = customers.find(id).orElseThrow(() -> notFound("Customer " + id));
        var summary = customers.findSummary(id).orElseThrow(() -> notFound("Customer " + id));
        List<TerminalSummary> list = terminals.findPage(new Filter(null, null, null, null, id), null, 500);
        return new CustomerDetail(rec.id(), rec.name(), rec.segment(), rec.region(), rec.accountManager(),
                summary.terminals(), summary.activeTerminals(), summary.mrrCents(), rec.contactEmail(),
                rec.createdAt(), list);
    }
}
