'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Skill, SkillFileEntry } from '@runkora/shared/api';
import { fetchSkillFile } from '../_services/skills-api';
import type { SkillsLoadError } from '../_types';

export const SKILL_MD = 'SKILL.md';

export interface SkillFilesState {
  /** SKILL.md first, then the skill's other files. */
  files: SkillFileEntry[];
  selected: SkillFileEntry;
  /** Undefined while the selected file loads or after it failed. */
  content: string | undefined;
  failure: SkillsLoadError | undefined;
  open(path: string): void;
}

const byteLength = (text: string) => new TextEncoder().encode(text).length;

/** SKILL.md arrives with the page; each other file loads through the BFF once, when first opened. */
export function useSkillFiles(skill: Skill): SkillFilesState {
  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  const [contents, setContents] = useState<Record<string, string>>({ [SKILL_MD]: skill.content });
  const [failures, setFailures] = useState<Record<string, SkillsLoadError | undefined>>({});
  const loading = useRef(new Set<string>());
  const requests = useRef<AbortController | null>(null);
  const files = useMemo(
    () => [{ path: SKILL_MD, size: byteLength(skill.content) }, ...skill.files],
    [skill],
  );

  // Leaving the page cancels the file loads still in flight.
  useEffect(() => {
    const controller = new AbortController();
    requests.current = controller;
    return () => controller.abort();
  }, []);

  const open = useCallback(
    (file: string) => {
      setSelectedPath(file);
      const controller = requests.current;
      if (!controller || file in contents || loading.current.has(file)) return;
      loading.current.add(file);
      setFailures((prev) => ({ ...prev, [file]: undefined }));
      void fetchSkillFile(
        { marketplace: skill.marketplace, path: skill.path, file },
        controller.signal,
      ).then((result) => {
        loading.current.delete(file);
        if (controller.signal.aborted) return;
        if (result.data) setContents((prev) => ({ ...prev, [file]: result.data.content }));
        else setFailures((prev) => ({ ...prev, [file]: result.error }));
      });
    },
    [contents, skill],
  );

  const selected = files.find((file) => file.path === selectedPath) ?? files[0];
  return {
    files,
    selected,
    content: contents[selected.path],
    failure: failures[selected.path],
    open,
  };
}
