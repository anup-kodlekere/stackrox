import React from 'react';
import { Card, CardBody, CardTitle, Flex, Grid, GridItem, Text } from '@patternfly/react-core';

import SeverityIcons from 'Components/PatternFly/SeverityIcons';

import { VulnerabilitySeverity } from 'types/cve.proto';
import { vulnerabilitySeverityLabels } from 'messages/common';

export type BySeveritySummaryCardProps = {
    title: string;
    severityCounts: Record<VulnerabilitySeverity, number>;
    hiddenSeverities: Set<VulnerabilitySeverity>;
};

const severitiesCriticalToLow = [
    'CRITICAL_VULNERABILITY_SEVERITY',
    'IMPORTANT_VULNERABILITY_SEVERITY',
    'MODERATE_VULNERABILITY_SEVERITY',
    'LOW_VULNERABILITY_SEVERITY',
] as const;

const disabledColor = 'var(--pf-global--disabled-color--100)';

function BySeveritySummaryCard({
    title,
    severityCounts,
    hiddenSeverities,
}: BySeveritySummaryCardProps) {
    return (
        <Card isCompact>
            <CardTitle>{title}</CardTitle>
            <CardBody>
                <Grid className="pf-u-pl-sm">
                    {severitiesCriticalToLow.map((severity) => {
                        const count = severityCounts[severity];
                        const hasNoResults = count === 0;
                        const isHidden = hiddenSeverities.has(severity);

                        let textColor = '';
                        let text = `${count} ${vulnerabilitySeverityLabels[severity]}`;

                        if (isHidden) {
                            textColor = disabledColor;
                            text = 'Results hidden';
                        } else if (hasNoResults) {
                            textColor = disabledColor;
                            text = 'No results';
                        }

                        const Icon = SeverityIcons[severity];

                        return (
                            <GridItem key={severity} span={6}>
                                <Flex
                                    className="pf-u-pt-sm"
                                    spaceItems={{ default: 'spaceItemsSm' }}
                                    alignItems={{ default: 'alignItemsCenter' }}
                                >
                                    <Icon
                                        color={hasNoResults || isHidden ? textColor : undefined}
                                    />
                                    <Text style={{ color: textColor }}>{text}</Text>
                                </Flex>
                            </GridItem>
                        );
                    })}
                </Grid>
            </CardBody>
        </Card>
    );
}

export default BySeveritySummaryCard;
