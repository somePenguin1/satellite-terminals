package dev.satfleet.fleetapi.deviceconfig;

import static dev.satfleet.fleetapi.common.ApiExceptions.badRequest;
import static dev.satfleet.fleetapi.common.ApiExceptions.conflict;
import static dev.satfleet.fleetapi.common.ApiExceptions.notFound;
import static dev.satfleet.fleetapi.common.ApiExceptions.preconditionFailed;

import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigChange;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.DeviceConfig;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ExportRow;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.HistoryEntry;
import java.util.List;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class DeviceConfigService {

    private final DeviceConfigRepository repo;

    public DeviceConfigService(DeviceConfigRepository repo) {
        this.repo = repo;
    }

    public DeviceConfig get(String terminalId) {
        return repo.find(terminalId).orElseThrow(() -> notFound("Configuration for terminal " + terminalId));
    }

    /** Optimistic-concurrency update: callers pass the version they read (from If-Match). */
    @Transactional
    public DeviceConfig update(String terminalId, int expectedVersion, ConfigChange change, String actor) {
        DeviceConfig current = get(terminalId);
        if ("decommissioned".equals(current.terminalStatus())) {
            throw conflict(terminalId + " is decommissioned; its configuration is read-only");
        }
        if (change.windowStart().equals(change.windowEnd())) {
            throw badRequest("The maintenance window must start and end at different times");
        }
        if (repo.update(terminalId, expectedVersion, change, actor) == 0) {
            throw preconditionFailed("Configuration changed since revision " + expectedVersion
                    + "; the current revision is " + get(terminalId).version() + ". Reload and reapply your change.");
        }
        return get(terminalId);
    }

    public List<HistoryEntry> history(String terminalId) {
        get(terminalId); // 404 for unknown terminals rather than an empty list
        return repo.history(terminalId, 100);
    }

    public List<ExportRow> export(String afterId, int limit) {
        return repo.export(afterId, limit);
    }
}
