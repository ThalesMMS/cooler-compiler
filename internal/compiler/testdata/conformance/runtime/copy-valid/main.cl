class Cell {
  value : Int <- 1;
  set(next : Int) : SELF_TYPE {{ value <- next; self; }};
  get() : Int { value };
};
class Holder {
  cell : Cell <- new Cell;
  get() : Cell { cell };
};
class FancyHolder inherits Holder {};
class Main inherits IO {
  emit(value : Bool) : Object { if value then out_string("T") else out_string("F") fi };
  main() : Object {
    let original : Holder <- new FancyHolder, clone : Holder <- original.copy() in {
      original.get().set(7);
      out_int(clone.get().get());
      emit(original = clone);
      emit(original.get() = clone.get());
      out_string(clone.type_name());
    }
  };
};
