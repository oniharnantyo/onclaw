import { useCallback, useEffect, useState } from "react";
import type { ReactElement } from "react";

import type { Skill } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import {
  EmptyState,
  LoadingHint,
  PaneHeader,
  SettingsList,
  SettingsRow,
} from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { Icon } from "../../../components/ui/Icon";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import type { SettingsPaneConfig } from "../types";

/** SkillsPane: directory-backed skill listing with a global enable/disable
 * toggle per skill (task 11.2). Directory-scanned rows carry source
 * "directory"; the chip renders whatever source the backend reports. The
 * empty state offers a next step (task 4.8): rescan the skills directory. */
export default function SkillsPane({
  pane,
}: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setLoadError(null);

    try {
      setSkills(await client.settings.skills());
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = async (skill: Skill, enabled: boolean): Promise<void> => {
    await client.settings.setSkillEnabled(skill.Name, enabled);
    setSkills((prev: Skill[]) =>
      prev.map((s: Skill) =>
        s.Name === skill.Name ? { ...s, Enabled: enabled } : s
      )
    );
  };

  return (
    <div data-testid="skills-pane">
      <PaneHeader description={pane.description} />

      {loading ? (
        <LoadingHint testId="skills-loading" />
      ) : loadError ? (
        <div role="alert">
          <Alert tone="danger" testId="skills-error">
            {loadError}
          </Alert>
        </div>
      ) : skills.length === 0 ? (
        <EmptyState
          title="No skills found in the skills directory."
          description="Drop a skill folder into the directory, then rescan."
          icon={<Icon name="spark" size={18} />}
          variant="boxed"
          testId="skills-empty"
          action={
            <Button
              variant="ghost"
              size="sm"
              data-testid="skills-reload"
              onClick={() => void load()}
            >
              Rescan directory
            </Button>
          }
        />
      ) : (
        <SettingsList testId="skill-list">
          {skills.map((skill: Skill) => (
            <SettingsRow
              key={skill.Name}
              title={skill.Name}
              testId={`skill-${skill.Name}`}
              badges={
                <span
                  className="inline-flex"
                  data-testid={`skill-source-${skill.Name}`}
                >
                  <Chip>{skill.Source}</Chip>
                </span>
              }
              detail={skill.Description}
              actions={
                <Toggle
                  on={skill.Enabled}
                  testId={`skill-toggle-${skill.Name}`}
                  label={`Enable ${skill.Name}`}
                  onChange={(next: boolean) => void toggle(skill, next)}
                />
              }
            />
          ))}
        </SettingsList>
      )}
    </div>
  );
}
