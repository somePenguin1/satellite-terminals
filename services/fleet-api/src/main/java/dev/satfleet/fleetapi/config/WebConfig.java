package dev.satfleet.fleetapi.config;

import io.swagger.v3.oas.models.OpenAPI;
import io.swagger.v3.oas.models.info.Info;
import org.springdoc.core.models.GroupedOpenApi;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.http.HttpHeaders;
import org.springframework.scheduling.annotation.EnableScheduling;
import org.springframework.web.servlet.config.annotation.CorsRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

@Configuration
public class WebConfig implements WebMvcConfigurer {

    private final String[] corsOrigins;

    public WebConfig(@Value("${fleet.cors-origins:}") String[] corsOrigins) {
        this.corsOrigins = corsOrigins;
    }

    @Override
    public void addCorsMappings(CorsRegistry registry) {
        if (corsOrigins.length == 0) {
            return;
        }
        registry.addMapping("/api/**")
                .allowedOrigins(corsOrigins)
                .allowedMethods("GET", "POST", "PUT", "PATCH", "DELETE")
                .exposedHeaders(HttpHeaders.ETAG, HttpHeaders.LOCATION, "X-Total-Count");
    }

    @Bean
    OpenAPI fleetOpenApi() {
        return new OpenAPI().info(new Info()
                .title("Fleet API")
                .version("1.0")
                .description("Terminals, device configuration, alerts and sales analytics for the satellite fleet. "
                        + "v2 endpoints use cursor pagination and a grouped configuration schema; v1 remains supported."));
    }

    @Bean
    GroupedOpenApi v1Api() {
        return GroupedOpenApi.builder().group("v1").pathsToMatch("/api/v1/**").build();
    }

    @Bean
    GroupedOpenApi v2Api() {
        return GroupedOpenApi.builder().group("v2").pathsToMatch("/api/v2/**").build();
    }

    /** Background jobs (alerts, partition maintenance) can be switched off, e.g. in tests. */
    @Configuration
    @EnableScheduling
    @ConditionalOnProperty(name = "fleet.scheduling.enabled", havingValue = "true", matchIfMissing = true)
    static class SchedulingConfig {}
}
