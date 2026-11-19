package dev.satfleet.fleetapi.terminals;

import static dev.satfleet.fleetapi.common.ApiExceptions.badRequest;
import static dev.satfleet.fleetapi.common.ApiExceptions.conflict;
import static dev.satfleet.fleetapi.common.ApiExceptions.notFound;

import dev.satfleet.fleetapi.customers.CustomerRepository;
import dev.satfleet.fleetapi.plans.Plan;
import dev.satfleet.fleetapi.plans.PlanController.PlanRepository;
import dev.satfleet.fleetapi.terminals.TerminalModels.ProvisionRequest;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalDetail;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalPatch;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class TerminalService {

    private final TerminalRepository terminals;
    private final PlanRepository plans;
    private final CustomerRepository customers;

    public TerminalService(TerminalRepository terminals, PlanRepository plans, CustomerRepository customers) {
        this.terminals = terminals;
        this.plans = plans;
        this.customers = customers;
    }

    public TerminalDetail get(String id) {
        return terminals.find(id).orElseThrow(() -> notFound("Terminal " + id));
    }

    /** Applies a lifecycle change and/or plan change atomically. */
    @Transactional
    public TerminalDetail patch(String id, TerminalPatch patch) {
        String current = terminals.findStatus(id).orElseThrow(() -> notFound("Terminal " + id)); // row-locked
        if ("decommissioned".equals(current)) {
            throw conflict(id + " is decommissioned and can no longer be changed");
        }
        if (patch.planCode() != null) {
            Plan target = plans.find(patch.planCode())
                    .orElseThrow(() -> badRequest("Unknown plan " + patch.planCode()));
            Plan existing = get(id).plan();
            if (!target.family().equals(existing.family())) {
                throw badRequest("Plan " + target.name() + " is for " + target.family()
                        + " customers; this terminal is on a " + existing.family() + " plan");
            }
            terminals.updatePlan(id, target.code());
        }
        if (patch.status() != null && !patch.status().equals(current)) {
            if (!TerminalLifecycle.canTransition(current, patch.status())) {
                throw conflict("Cannot move a terminal from " + current + " to " + patch.status());
            }
            terminals.updateStatus(id, patch.status());
        }
        return get(id);
    }

    @Transactional
    public TerminalDetail provision(ProvisionRequest req, String actor) {
        var customer = customers.findSummary(req.customerId())
                .orElseThrow(() -> badRequest("Customer " + req.customerId() + " does not exist"));
        Plan plan = plans.find(req.planCode()).orElseThrow(() -> badRequest("Unknown plan " + req.planCode()));
        boolean maritime = "maritime".equals(customer.segment());
        String expectedFamily = maritime ? "maritime" : "residential".equals(customer.segment()) ? "residential" : "business";
        if (!plan.family().equals(expectedFamily)) {
            throw badRequest(plan.name() + " is not available to " + customer.segment() + " customers");
        }
        String qos = switch (plan.tier()) {
            case 1 -> "standard";
            case 2 -> "priority";
            default -> "business-critical";
        };
        String id = terminals.insert(req.customerId(), plan, req.model(), customer.region(),
                req.latitude(), req.longitude(), maritime ? 10_000 : 5_000, qos, actor);
        return get(id);
    }
}
